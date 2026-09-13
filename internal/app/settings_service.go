package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/AntNoHuabei/blankmind/internal/credential"
)

// credentialTarget 返回某服务商在系统凭据中的目标名。
func credentialTarget(id int64) string {
	return "BlankMind/provider/" + strconv.FormatInt(id, 10)
}

var (
	credentialSet = credential.Set
	credentialGet = credential.Get
)

// resolveSecret 把服务商的 API key 解析到内存,并返回是否需要清理数据库明文:
//   - 列中仍为明文(旧数据或非 Windows):尝试迁入系统凭据并清列;
//   - 列已清空:从系统凭据读取填充。
func resolveSecret(p *Provider) bool {
	if p == nil {
		return false
	}
	key := strings.TrimSpace(p.APIKey)
	if key != "" {
		return credentialSet(credentialTarget(p.ID), key) == nil
	}
	if k, err := credentialGet(credentialTarget(p.ID)); err == nil && k != "" {
		p.APIKey = k
	}
	return false
}

// SettingsService 负责模型服务商(Provider)与应用设置(key-value)的读写。
//
// 同时向 Agent 层暴露“默认 Provider”查询,供模型路由使用。
type SettingsService struct {
	db     *sql.DB
	notify func(name string, data any)
}

// NewSettingsService 构造 SettingsService。
func NewSettingsService(db *sql.DB) *SettingsService {
	return &SettingsService{db: db}
}

// setNotify 注入模型配置变更事件出口。
func (s *SettingsService) setNotify(fn func(name string, data any)) {
	s.notify = fn
}

func (s *SettingsService) notifyModelsChanged() {
	if s.notify != nil {
		s.notify("models.changed", "")
	}
}

// ErrNotFound 表示目标记录不存在。
var ErrNotFound = errors.New("record not found")

// 设置项 key 常量。
const (
	SettingTheme           = "theme"             // 皮肤 id
	SettingCaptureHotkey   = "hotkey.capture"    // 截图全局热键,如 "ctrl+alt+s"
	SettingRemindEnabled   = "remind.enabled"    // "1"/"0"
	SettingRemindLeadHours = "remind.lead.hours" // 提前提醒小时数
	SettingDefaultProvider = "provider.default"  // 默认 Provider 显示名(冗余 is_default)
	SettingScreenshotDir   = "screenshot.dir"    // 截图保存目录(空 = 应用数据目录)
)

// HasProviders 返回是否已配置至少一个模型服务商(首启向导判定)。
func (s *SettingsService) HasProviders() (bool, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM providers").Scan(&n)
	return n > 0, err
}

// ListProviders 返回全部服务商,默认项排最前。
func (s *SettingsService) ListProviders() ([]Provider, error) {
	rows, err := s.db.Query(`
		SELECT id, name, kind, base_url, api_key, model, multimodal, is_default, created_at
		FROM providers ORDER BY is_default DESC, id ASC`)
	if err != nil {
		return nil, err
	}
	items := []Provider{}
	migratedIDs := []int64{}
	for rows.Next() {
		var p Provider
		var mm, def int
		if err := rows.Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey,
			&p.Model, &mm, &def, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Multimodal = mm != 0
		p.IsDefault = def != 0
		if resolveSecret(&p) {
			migratedIDs = append(migratedIDs, p.ID)
		}
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// 单连接 SQLite 必须在 rows 关闭后再写,否则会等待自己占用的连接。
	for _, id := range migratedIDs {
		_, _ = s.db.Exec("UPDATE providers SET api_key = '' WHERE id = ?", id)
	}
	return items, nil
}

// SaveProvider 新增或更新服务商;若传了 Models(启用模型集合)则全量替换其集合,
// 并保证 providers.model 落在集合内;若标记为默认会清除其它默认标记。
func (s *SettingsService) SaveProvider(in ProviderInput) (Provider, error) {
	name := strings.TrimSpace(in.Name)
	kind := strings.TrimSpace(in.Kind)
	if name == "" || kind == "" {
		return Provider{}, errors.New("name 与 kind 不能为空")
	}
	cur := strings.TrimSpace(in.Model)
	// 归一化启用集合:传入为空则以当前模型为唯一启用项
	models := in.Models
	if len(models) == 0 && cur != "" {
		models = []ProviderModelInput{{Model: cur, Multimodal: in.Multimodal}}
	}
	seen := map[string]bool{}
	dedup := make([]ProviderModelInput, 0, len(models))
	for _, m := range models {
		mmName := strings.TrimSpace(m.Model)
		if mmName == "" || seen[mmName] {
			continue
		}
		seen[mmName] = true
		multimodal := m.Multimodal
		if catalogMultimodal, found := modelMultimodal(kind, mmName); found {
			multimodal = catalogMultimodal
		}
		dedup = append(dedup, ProviderModelInput{
			Model: mmName, Label: strings.TrimSpace(m.Label), Custom: m.Custom, Multimodal: multimodal,
		})
	}
	models = dedup
	if cur == "" && len(models) > 0 {
		cur = models[0].Model
	}
	currentMultimodal := in.Multimodal
	for _, model := range models {
		if model.Model == cur {
			currentMultimodal = model.Multimodal
			break
		}
	}
	if catalogMultimodal, found := modelMultimodal(kind, cur); found {
		currentMultimodal = catalogMultimodal
	}
	mm := boolToInt(currentMultimodal)

	tx, err := s.db.Begin()
	if err != nil {
		return Provider{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	// 默认服务商语义:由“当前模型切换”或显式指定产生。
	//   - update:保留原有默认标记,除非显式请求设为默认(清其它并置 1);
	//   - insert:全局尚无默认时自动成为默认。
	isDefaultCol := false
	if in.ID > 0 {
		var orig int
		_ = tx.QueryRow("SELECT is_default FROM providers WHERE id = ?", in.ID).Scan(&orig)
		isDefaultCol = orig != 0
		if in.IsDefault {
			isDefaultCol = true
		}
	} else if in.IsDefault {
		isDefaultCol = true
	} else {
		var def int
		_ = tx.QueryRow("SELECT COUNT(*) FROM providers WHERE is_default = 1").Scan(&def)
		isDefaultCol = def == 0
	}
	if isDefaultCol {
		if _, err := tx.Exec("UPDATE providers SET is_default = 0"); err != nil {
			return Provider{}, err
		}
	}
	var res sql.Result
	ts := now()
	keyVal := strings.TrimSpace(in.APIKey)
	if in.ID > 0 {
		if keyVal != "" {
			res, err = tx.Exec(`
				UPDATE providers SET name=?, kind=?, base_url=?, api_key=?, model=?,
					multimodal=?, is_default=?
				WHERE id=?`,
				name, kind, strings.TrimSpace(in.BaseURL), keyVal, cur,
				mm, boolToInt(isDefaultCol), in.ID)
		} else {
			// 留空 key = 沿用原有(不覆写列;凭据仍在则从系统凭据回填)
			res, err = tx.Exec(`
				UPDATE providers SET name=?, kind=?, base_url=?, model=?,
					multimodal=?, is_default=?
				WHERE id=?`,
				name, kind, strings.TrimSpace(in.BaseURL), cur,
				mm, boolToInt(isDefaultCol), in.ID)
		}
	} else {
		res, err = tx.Exec(`
			INSERT INTO providers (name, kind, base_url, api_key, model, multimodal, is_default, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			name, kind, strings.TrimSpace(in.BaseURL), keyVal, cur,
			mm, boolToInt(isDefaultCol), ts)
	}
	if err != nil {
		return Provider{}, err
	}
	id := in.ID
	if id == 0 {
		id, err = res.LastInsertId()
		if err != nil {
			return Provider{}, err
		}
	}

	// 全量替换启用模型集合,并确保当前模型在集合内
	if _, err := tx.Exec("DELETE FROM provider_models WHERE provider_id = ?", id); err != nil {
		return Provider{}, err
	}
	for _, m := range models {
		cus := 0
		if m.Custom {
			cus = 1
		}
		if _, err := tx.Exec(`
			INSERT INTO provider_models (provider_id, model, label, custom, multimodal, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, id, m.Model, m.Label, cus, boolToInt(m.Multimodal), ts); err != nil {
			return Provider{}, err
		}
	}
	if cur != "" && !seen[cur] {
		cus := 0
		if kind == "custom" {
			cus = 1
		}
		if _, err := tx.Exec(`
			INSERT OR IGNORE INTO provider_models (provider_id, model, label, custom, multimodal, created_at)
			VALUES (?, ?, '', ?, ?, ?)`, id, cur, cus, mm, ts); err != nil {
			return Provider{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Provider{}, err
	}
	provider, err := s.getProvider(id)
	if err != nil {
		return Provider{}, err
	}
	s.notifyModelsChanged()
	return provider, nil
}

// ProviderModels 返回服务商已启用的模型集合。
func (s *SettingsService) ProviderModels(id int64) ([]ProviderModel, error) {
	rows, err := s.db.Query(`
		SELECT model, label, custom, multimodal FROM provider_models
		WHERE provider_id = ? ORDER BY custom ASC, model ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ProviderModel{}
	for rows.Next() {
		var m ProviderModel
		var c, mm int
		if err := rows.Scan(&m.Model, &m.Label, &c, &mm); err != nil {
			return nil, err
		}
		m.Custom = c != 0
		m.Multimodal = mm != 0
		items = append(items, m)
	}
	return items, rows.Err()
}

// ModelOptions 返回“服务商 × 启用模型”扁平列表(对话页模型切换用)。
// IsDefault=true 表示该行是当前默认使用的模型。
func (s *SettingsService) ModelOptions() ([]ModelOption, error) {
	rows, err := s.db.Query(`
		SELECT p.id, p.name, p.kind, p.model, pm.model, pm.label, pm.custom, pm.multimodal
		FROM providers p
		JOIN provider_models pm ON pm.provider_id = p.id
		ORDER BY p.is_default DESC, p.id ASC, pm.custom ASC, pm.model ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ModelOption{}
	for rows.Next() {
		var o ModelOption
		var activeModel string
		var c, mm int
		if err := rows.Scan(&o.ProviderID, &o.ProviderName, &o.Kind, &activeModel,
			&o.Model, &o.Label, &c, &mm); err != nil {
			return nil, err
		}
		o.Custom = c != 0
		o.Multimodal = mm != 0
		o.IsDefault = o.Model == activeModel
		items = append(items, o)
	}
	return items, rows.Err()
}

// SetProviderModel 把某服务商下的某启用模型设为“当前使用模型”,并使其服务商为默认。
func (s *SettingsService) SetProviderModel(providerID int64, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("模型不能为空")
	}
	provider, err := s.getProvider(providerID)
	if err != nil {
		return err
	}
	var storedMultimodal int
	if err := s.db.QueryRow(`
		SELECT multimodal FROM provider_models WHERE provider_id = ? AND model = ?`,
		providerID, model).Scan(&storedMultimodal); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("该模型未在此服务商下启用,请先在设置中启用")
		}
		return err
	}
	multimodal, err := s.providerModelMultimodal(provider, model, storedMultimodal != 0)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec("UPDATE providers SET is_default = 0"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE provider_models SET multimodal = ? WHERE provider_id = ? AND model = ?`,
		boolToInt(multimodal), providerID, model); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"UPDATE providers SET model = ?, multimodal = ?, is_default = 1 WHERE id = ?",
		model, boolToInt(multimodal), providerID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyModelsChanged()
	return nil
}

func (s *SettingsService) providerModelMultimodal(provider Provider, model string, stored bool) (bool, error) {
	if multimodal, found := modelMultimodal(provider.Kind, model); found {
		return multimodal, nil
	}
	if !strings.EqualFold(strings.TrimSpace(provider.Kind), "herdsman") {
		return stored, nil
	}
	models, err := s.DiscoverProviderModels(ProviderInput{
		ID: provider.ID, Name: provider.Name, Kind: provider.Kind, BaseURL: provider.BaseURL,
		APIKey: provider.APIKey, Model: model,
	})
	if err != nil {
		return false, fmt.Errorf("获取 Herdsman 模型能力失败: %w", err)
	}
	for _, candidate := range models {
		if strings.EqualFold(candidate.ID, model) {
			return candidate.Multimodal, nil
		}
	}
	return false, fmt.Errorf("Herdsman 未返回模型 %q", model)
}

// EnableModel 启用某服务商下的一个模型(设置页即时开关用)。
// 内置模型传 catalog label;自定义模型由前端传入。
func (s *SettingsService) EnableModel(providerID int64, model, label string, custom bool) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("模型不能为空")
	}
	provider, err := s.getProvider(providerID)
	if err != nil {
		return err
	}
	multimodal, _ := modelMultimodal(provider.Kind, model)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var n int
	_ = tx.QueryRow("SELECT COUNT(*) FROM providers WHERE id = ?", providerID).Scan(&n)
	if n == 0 {
		return errors.New("服务商不存在")
	}
	cus := 0
	if custom {
		cus = 1
	}
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO provider_models (provider_id, model, label, custom, multimodal, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, providerID, model, label, cus, boolToInt(multimodal), now()); err != nil {
		return err
	}
	// 若该服务商尚无当前模型,自动把新启用模型设为当前
	var cur string
	_ = tx.QueryRow("SELECT model FROM providers WHERE id = ?", providerID).Scan(&cur)
	if strings.TrimSpace(cur) == "" {
		if _, err := tx.Exec("UPDATE providers SET model = ?, multimodal = ? WHERE id = ?",
			model, boolToInt(multimodal), providerID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyModelsChanged()
	return nil
}

// DisableModel 停用某服务商下的一个模型;停用其“当前模型”时自动切到其它启用模型,
// 若这是最后一个可用模型则拒绝。
func (s *SettingsService) DisableModel(providerID int64, model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("模型不能为空")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	var total int
	_ = tx.QueryRow(`
		SELECT COUNT(*) FROM provider_models WHERE provider_id = ?`, providerID).Scan(&total)
	if total <= 1 {
		return errors.New("至少保留一个可用模型")
	}
	var cur string
	_ = tx.QueryRow("SELECT model FROM providers WHERE id = ?", providerID).Scan(&cur)
	if model == cur {
		var fallback string
		var fallbackMultimodal int
		_ = tx.QueryRow(`
			SELECT model, multimodal FROM provider_models WHERE provider_id = ? AND model <> ?
			ORDER BY custom ASC, model ASC LIMIT 1`, providerID, model).Scan(&fallback, &fallbackMultimodal)
		if fallback == "" {
			return errors.New("无法停用最后一个可用模型")
		}
		if _, err := tx.Exec("UPDATE providers SET model = ?, multimodal = ? WHERE id = ?",
			fallback, fallbackMultimodal, providerID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		DELETE FROM provider_models WHERE provider_id = ? AND model = ?`, providerID, model); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notifyModelsChanged()
	return nil
}

// getProvider 按 ID 读取单个 Provider。
func (s *SettingsService) getProvider(id int64) (Provider, error) {
	var p Provider
	var mm, def int
	err := s.db.QueryRow(`
		SELECT id, name, kind, base_url, api_key, model, multimodal, is_default, created_at
		FROM providers WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey, &p.Model, &mm, &def, &p.CreatedAt)
	if err != nil {
		return p, err
	}
	p.Multimodal = mm != 0
	p.IsDefault = def != 0
	if resolveSecret(&p) {
		_, _ = s.db.Exec("UPDATE providers SET api_key = '' WHERE id = ?", p.ID)
	}
	return p, nil
}

// DeleteProvider 删除一个服务商。
func (s *SettingsService) DeleteProvider(id int64) error {
	res, err := s.db.Exec("DELETE FROM providers WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// 清理系统凭据(尽力而为,不存在也不报错)
	_ = credential.Delete(credentialTarget(id))
	s.notifyModelsChanged()
	return nil
}

// ProviderTemplates 返回内置厂商模板(设置向导快速填充)。
func (s *SettingsService) ProviderTemplates() []ProviderTemplate {
	return []ProviderTemplate{
		{
			Name: "DeepSeek", Kind: "deepseek",
			BaseURL: "https://api.deepseek.com", Model: "deepseek-flash",
			Multimodal: true, DocsURL: "https://platform.deepseek.com",
		},
		{
			Name: "OpenAI", Kind: "openai",
			BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini",
			Multimodal: true, DocsURL: "https://platform.openai.com",
		},
		{
			Name: "通义千问", Kind: "qwen",
			BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			Model:   "qwen-plus", Multimodal: false, DocsURL: "https://help.aliyun.com/zh/model-studio",
		},
		{
			Name: "Moonshot", Kind: "kimi",
			BaseURL: "https://api.moonshot.cn/v1", Model: "kimi-k2-0711-preview",
			Multimodal: false, DocsURL: "https://platform.moonshot.cn",
		},
		{
			Name: "Herdsman", Kind: "herdsman",
			BaseURL: "http://localhost:8080/v1", Model: "",
			Multimodal: false, DocsURL: "",
		},
		{
			Name: "Ollama(本地)", Kind: "custom",
			BaseURL: "http://localhost:11434/v1", Model: "llama3.2",
			Multimodal: false, DocsURL: "https://ollama.com",
		},
		{
			Name: "自定义服务商", Kind: "custom",
			BaseURL: "https://api.example.com/v1", Model: "",
			Multimodal: true, DocsURL: "",
		},
	}
}

// ListSettings 返回全部设置项。
func (s *SettingsService) ListSettings() ([]Setting, error) {
	rows, err := s.db.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Setting{}
	for rows.Next() {
		var st Setting
		if err := rows.Scan(&st.Key, &st.Value); err != nil {
			return nil, err
		}
		items = append(items, st)
	}
	return items, rows.Err()
}

// GetSetting 读取设置项;不存在返回空串与 nil。
func (s *SettingsService) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetSetting 写入设置项(upsert)。
func (s *SettingsService) SetSetting(key, value string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("key 不能为空")
	}
	_, err := s.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// boolToInt 转换布尔值为 SQLite 整数。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
