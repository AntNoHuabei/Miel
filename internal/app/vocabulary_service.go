package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxVocabularyTermRunes = 200

// VocabularyService owns saved terms and their review progress.
type VocabularyService struct {
	db     *sql.DB
	Notify func(name string, data any)
}

type vocabularyStore interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func NewVocabularyService(db *sql.DB) *VocabularyService {
	return &VocabularyService{db: db}
}

var vocabularySvc *VocabularyService

func normalizeVocabularyTerm(value string) (string, error) {
	cleaned := strings.Map(func(char rune) rune {
		if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || unicode.IsSpace(char) {
			return char
		}
		return -1
	}, value)
	term := strings.Join(strings.Fields(cleaned), " ")
	if term == "" {
		return "", errors.New("生词必须包含英文字母")
	}
	if utf8.RuneCountInString(term) > maxVocabularyTermRunes {
		return "", fmt.Errorf("生词不能超过 %d 个字符", maxVocabularyTermRunes)
	}
	return term, nil
}

func (s *VocabularyService) notify(word VocabularyWord) {
	if s.Notify != nil {
		s.Notify("vocabulary.changed", word)
	}
}

func (s *VocabularyService) ListWords(query string) ([]VocabularyWord, error) {
	like := "%" + strings.TrimSpace(query) + "%"
	rows, err := s.db.Query(`
		SELECT id, term, meaning, example, source, created_at, updated_at,
			review_count, mastered_count, again_count, last_reviewed_at
		FROM vocabulary_words
		WHERE ? = '%%' OR term LIKE ? OR meaning LIKE ? OR example LIKE ?
		ORDER BY updated_at DESC, id DESC`, like, like, like, like)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []VocabularyWord{}
	for rows.Next() {
		word, err := scanVocabularyWord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, word)
	}
	return items, rows.Err()
}

func scanVocabularyWord(scanner interface{ Scan(...any) error }) (VocabularyWord, error) {
	var word VocabularyWord
	err := scanner.Scan(&word.ID, &word.Term, &word.Meaning, &word.Example, &word.Source,
		&word.CreatedAt, &word.UpdatedAt, &word.ReviewCount, &word.MasteredCount,
		&word.AgainCount, &word.LastReviewedAt)
	return word, err
}

func (s *VocabularyService) GetWord(id int64) (VocabularyWord, error) {
	if id <= 0 {
		return VocabularyWord{}, errors.New("缺少生词 ID")
	}
	return scanVocabularyWord(s.db.QueryRow(`
		SELECT id, term, meaning, example, source, created_at, updated_at,
			review_count, mastered_count, again_count, last_reviewed_at
		FROM vocabulary_words WHERE id = ?`, id))
}

// AddWord inserts a term or returns the existing case-insensitive match.
func (s *VocabularyService) AddWord(in VocabularyWordInput) (VocabularyWord, error) {
	word, err := addVocabularyWord(s.db, in)
	if err == nil {
		s.notify(word)
	}
	return word, err
}

// AddWords validates the full batch before atomically inserting or merging it.
func (s *VocabularyService) AddWords(inputs []VocabularyWordInput) ([]VocabularyWord, error) {
	if len(inputs) == 0 {
		return nil, errors.New("批量添加至少需要一条生词")
	}
	if len(inputs) > 100 {
		return nil, errors.New("每次最多批量添加 100 条生词")
	}
	prepared := make([]VocabularyWordInput, len(inputs))
	for index, input := range inputs {
		term, err := normalizeVocabularyTerm(input.Term)
		if err != nil {
			return nil, fmt.Errorf("第 %d 条生词无效: %w", index+1, err)
		}
		input.ID = 0
		input.Term = term
		prepared[index] = input
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck
	words := make([]VocabularyWord, 0, len(prepared))
	positions := make(map[int64]int, len(prepared))
	for _, input := range prepared {
		word, err := addVocabularyWord(tx, input)
		if err != nil {
			return nil, err
		}
		if position, exists := positions[word.ID]; exists {
			words[position] = word
			continue
		}
		positions[word.ID] = len(words)
		words = append(words, word)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, word := range words {
		s.notify(word)
	}
	return words, nil
}

func addVocabularyWord(store vocabularyStore, in VocabularyWordInput) (VocabularyWord, error) {
	term, err := normalizeVocabularyTerm(in.Term)
	if err != nil {
		return VocabularyWord{}, err
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "manual"
	}
	ts := now()
	_, err = store.Exec(`
		INSERT INTO vocabulary_words (term, meaning, example, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(term) DO UPDATE SET
			meaning = CASE WHEN excluded.meaning <> '' THEN excluded.meaning ELSE vocabulary_words.meaning END,
			example = CASE WHEN excluded.example <> '' THEN excluded.example ELSE vocabulary_words.example END,
			updated_at = excluded.updated_at`,
		term, strings.TrimSpace(in.Meaning), strings.TrimSpace(in.Example), source, ts, ts)
	if err != nil {
		return VocabularyWord{}, err
	}
	var id int64
	if err := store.QueryRow("SELECT id FROM vocabulary_words WHERE term = ? COLLATE NOCASE", term).Scan(&id); err != nil {
		return VocabularyWord{}, err
	}
	return scanVocabularyWord(store.QueryRow(`
		SELECT id, term, meaning, example, source, created_at, updated_at,
			review_count, mastered_count, again_count, last_reviewed_at
		FROM vocabulary_words WHERE id = ?`, id))
}

func (s *VocabularyService) AddSelectedText(text string) (VocabularyWord, error) {
	return s.AddWord(VocabularyWordInput{Term: text, Source: "selection"})
}

func (s *VocabularyService) UpdateWord(in VocabularyWordInput) (VocabularyWord, error) {
	if in.ID <= 0 {
		return VocabularyWord{}, errors.New("缺少生词 ID")
	}
	term, err := normalizeVocabularyTerm(in.Term)
	if err != nil {
		return VocabularyWord{}, err
	}
	result, err := s.db.Exec(`
		UPDATE vocabulary_words SET term = ?, meaning = ?, example = ?, updated_at = ?
		WHERE id = ?`, term, strings.TrimSpace(in.Meaning), strings.TrimSpace(in.Example), now(), in.ID)
	if err != nil {
		return VocabularyWord{}, err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return VocabularyWord{}, ErrNotFound
	}
	word, err := s.GetWord(in.ID)
	if err == nil {
		s.notify(word)
	}
	return word, err
}

func (s *VocabularyService) DeleteWord(id int64) error {
	word, err := s.GetWord(id)
	if err != nil {
		return err
	}
	result, err := s.db.Exec("DELETE FROM vocabulary_words WHERE id = ?", id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrNotFound
	}
	s.notify(VocabularyWord{ID: word.ID, Term: word.Term})
	return nil
}

func (s *VocabularyService) CreateReview(count int) (VocabularyReview, error) {
	if count < 1 || count > 8 {
		return VocabularyReview{}, errors.New("复习词数必须在 1 到 8 之间")
	}
	rows, err := s.db.Query(`
		SELECT id, term, meaning, example, source, created_at, updated_at,
			review_count, mastered_count, again_count, last_reviewed_at
		FROM vocabulary_words
		ORDER BY CASE WHEN review_count = 0 THEN 0 ELSE 1 END,
			(last_reviewed_at + ABS(RANDOM() % 86400)) ASC
		LIMIT ?`, count)
	if err != nil {
		return VocabularyReview{}, err
	}
	defer rows.Close()
	words := []VocabularyWord{}
	for rows.Next() {
		word, err := scanVocabularyWord(rows)
		if err != nil {
			return VocabularyReview{}, err
		}
		words = append(words, word)
	}
	if err := rows.Err(); err != nil {
		return VocabularyReview{}, err
	}
	if len(words) == 0 {
		return VocabularyReview{}, errors.New("生词本为空，请先添加生词")
	}
	return VocabularyReview{Words: words, Seed: now()}, nil
}

// RandomWords draws terms uniformly without changing review progress.
func (s *VocabularyService) RandomWords(count int) ([]VocabularyWord, error) {
	if count < 1 || count > 50 {
		return nil, errors.New("随机抽取词数必须在 1 到 50 之间")
	}
	rows, err := s.db.Query(`
		SELECT id, term, meaning, example, source, created_at, updated_at,
			review_count, mastered_count, again_count, last_reviewed_at
		FROM vocabulary_words
		ORDER BY RANDOM()
		LIMIT ?`, count)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	words := []VocabularyWord{}
	for rows.Next() {
		word, err := scanVocabularyWord(rows)
		if err != nil {
			return nil, err
		}
		words = append(words, word)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(words) == 0 {
		return nil, errors.New("生词本为空，请先添加生词")
	}
	return words, nil
}

func (s *VocabularyService) GenerateSentence(ids []int64) (string, error) {
	words, err := s.wordsByIDs(ids)
	if err != nil {
		return "", err
	}
	terms := make([]string, 0, len(words))
	for _, word := range words {
		terms = append(terms, word.Term)
	}
	provider, err := settingsSvc.DefaultProvider()
	if err != nil {
		return "", errors.New("尚未配置模型服务商，无法生成参考句")
	}
	prompt := "请使用下面每一个词或短语写一个自然、语法正确、便于语言学习的句子。只输出句子本身，不要解释、翻译、编号或引号。\n\n词语：" + strings.Join(terms, "；")
	return textOnce(provider, prompt)
}

func (s *VocabularyService) RecordReview(ids []int64, mastered bool) error {
	if len(ids) == 0 {
		return errors.New("请选择要记录的生词")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	column := "again_count"
	if mastered {
		column = "mastered_count"
	}
	for _, id := range uniquePositiveIDs(ids) {
		result, err := tx.Exec(`UPDATE vocabulary_words SET review_count = review_count + 1, `+column+` = `+column+` + 1, last_reviewed_at = ?, updated_at = ? WHERE id = ?`, now(), now(), id)
		if err != nil {
			return err
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return ErrNotFound
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.Notify != nil {
		s.Notify("vocabulary.changed", VocabularyWord{})
	}
	return nil
}

func (s *VocabularyService) Stats() (VocabularyStats, error) {
	var stats VocabularyStats
	err := s.db.QueryRow(`
		SELECT COUNT(*),
			COALESCE(SUM(CASE WHEN review_count = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN review_count > 0 AND NOT (mastered_count >= 2 AND mastered_count * 4 >= review_count * 3) THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN mastered_count >= 2 AND mastered_count * 4 >= review_count * 3 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(review_count), 0)
		FROM vocabulary_words`).Scan(&stats.Total, &stats.Unreviewed, &stats.Practising, &stats.Mastered, &stats.ReviewCount)
	return stats, err
}

func (s *VocabularyService) wordsByIDs(ids []int64) ([]VocabularyWord, error) {
	unique := uniquePositiveIDs(ids)
	if len(unique) == 0 {
		return nil, errors.New("请选择生词")
	}
	words := make([]VocabularyWord, 0, len(unique))
	for _, id := range unique {
		word, err := s.GetWord(id)
		if err != nil {
			return nil, err
		}
		words = append(words, word)
	}
	return words, nil
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			result = append(result, id)
		}
	}
	return result
}
