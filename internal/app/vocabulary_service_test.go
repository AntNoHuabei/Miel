package app

import (
	"testing"
)

func TestVocabularyLifecycleDedupAndReviewProgress(t *testing.T) {
	db := newTodoTestDB(t)
	service := NewVocabularyService(db)
	var notifications int
	service.Notify = func(name string, _ any) {
		if name == "vocabulary.changed" {
			notifications++
		}
	}

	first, err := service.AddSelectedText("  serendipity\r\n")
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := service.AddWord(VocabularyWordInput{Term: "SERENDIPITY", Meaning: "意外发现美好事物的运气"})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != first.ID || duplicate.Meaning == "" {
		t.Fatalf("duplicate was not merged: first=%#v duplicate=%#v", first, duplicate)
	}
	second, err := service.AddWord(VocabularyWordInput{Term: "meticulous", Example: "She kept meticulous notes."})
	if err != nil {
		t.Fatal(err)
	}

	items, err := service.ListWords("notes")
	if err != nil || len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("search result = %#v, err = %v", items, err)
	}
	review, err := service.CreateReview(2)
	if err != nil || len(review.Words) != 2 {
		t.Fatalf("review = %#v, err = %v", review, err)
	}
	if err := service.RecordReview([]int64{first.ID, second.ID, first.ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordReview([]int64{first.ID}, false); err != nil {
		t.Fatal(err)
	}
	updated, err := service.GetWord(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ReviewCount != 2 || updated.MasteredCount != 1 || updated.AgainCount != 1 {
		t.Fatalf("unexpected progress: %#v", updated)
	}
	stats, err := service.Stats()
	if err != nil || stats.Total != 2 || stats.ReviewCount != 3 {
		t.Fatalf("stats = %#v, err = %v", stats, err)
	}
	if err := service.DeleteWord(second.ID); err != nil {
		t.Fatal(err)
	}
	if notifications < 5 {
		t.Fatalf("notifications = %d, want lifecycle events", notifications)
	}
}

func TestVocabularyRejectsEmptyAndOversizedTerms(t *testing.T) {
	service := NewVocabularyService(newTodoTestDB(t))
	cleaned, err := service.AddSelectedText("  hello, 世界! 2026\r\nwell-being  ")
	if err != nil {
		t.Fatal(err)
	}
	if cleaned.Term != "hello wellbeing" {
		t.Fatalf("cleaned term = %q, want %q", cleaned.Term, "hello wellbeing")
	}
	if _, err := service.AddSelectedText("世界！2026"); err == nil {
		t.Fatal("term without English letters was accepted")
	}
	value := make([]rune, maxVocabularyTermRunes+1)
	for index := range value {
		value[index] = 'a'
	}
	if _, err := service.AddSelectedText(string(value)); err == nil {
		t.Fatal("oversized selected text was accepted")
	}
}

func TestVocabularyBatchAddIsAtomicAndMergesDuplicates(t *testing.T) {
	service := NewVocabularyService(newTodoTestDB(t))
	words, err := service.AddWords([]VocabularyWordInput{
		{Term: "  lucid!  ", Meaning: "清晰的"},
		{Term: "LUCID", Example: "She gave a lucid answer."},
		{Term: "resilient#2026", Meaning: "有韧性的"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != 2 || words[0].Term != "lucid" || words[0].Meaning == "" || words[0].Example == "" || words[1].Term != "resilient" {
		t.Fatalf("batch result = %#v", words)
	}
	if _, err := service.AddWords([]VocabularyWordInput{{Term: "valid"}, {Term: "世界！2026"}}); err == nil {
		t.Fatal("invalid batch was accepted")
	}
	items, err := service.ListWords("")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("invalid batch partially persisted: %#v", items)
	}
}

func TestVocabularySkillCommandsUseVocabularyService(t *testing.T) {
	todo := setupSkillCommandTest(t)
	oldVocabulary := vocabularySvc
	vocabularySvc = NewVocabularyService(store)
	t.Cleanup(func() { vocabularySvc = oldVocabulary })

	created := skillData[VocabularyWord](t, runSkill(t, todo, nil, skillRunRequest{
		Skill: "vocabulary", Command: "add", Args: []string{"--term", "ephemeral!!!2026中文", "--meaning", "短暂的"},
	}))
	if created.Term != "ephemeral" || created.Meaning != "短暂的" {
		t.Fatalf("created = %#v", created)
	}
	batch := skillData[[]VocabularyWord](t, runSkill(t, todo, nil, skillRunRequest{
		Skill: "vocabulary", Command: "add-many", Args: []string{"--items", `[{"term":"meticulous!","meaning":"细致的"},{"term":"resilient#2026","example":"She remained resilient."}]`},
	}))
	if len(batch) != 2 || batch[0].Term != "meticulous" || batch[1].Term != "resilient" {
		t.Fatalf("batch = %#v", batch)
	}
	review := skillData[VocabularyReview](t, runSkill(t, todo, nil, skillRunRequest{
		Skill: "vocabulary", Command: "review", Args: []string{"--count", "1"},
	}))
	if len(review.Words) != 1 {
		t.Fatalf("review = %#v", review)
	}
	random := skillData[[]VocabularyWord](t, runSkill(t, todo, nil, skillRunRequest{
		Skill: "vocabulary", Command: "random", Args: []string{"--count", "1"},
	}))
	if len(random) != 1 {
		t.Fatalf("random = %#v", random)
	}
	skillData[map[string]any](t, runSkill(t, todo, nil, skillRunRequest{
		Skill: "vocabulary", Command: "grade", Args: []string{"--ids", strconvID(created.ID), "--result", "mastered"},
	}))
	stats := skillData[VocabularyStats](t, runSkill(t, todo, nil, skillRunRequest{Skill: "vocabulary", Command: "stats"}))
	if stats.Total != 3 || stats.ReviewCount != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}
