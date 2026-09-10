package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/memory"
)

const SettingMemoryConfig = "memory.config.v1"

type MemoryConfig struct {
	Enabled      bool   `json:"enabled"`
	AutoExtract  bool   `json:"autoExtract"`
	Strategy     string `json:"strategy"`
	CustomPrompt string `json:"customPrompt"`
}

type MemoryConfigInput = MemoryConfig

type MemoryStatus struct {
	State         string `json:"state"`
	PendingJobs   int    `json:"pendingJobs"`
	LastSuccessAt string `json:"lastSuccessAt"`
	LastError     string `json:"lastError"`
}

type MemorySettingsView struct {
	Config          MemoryConfig               `json:"config"`
	Strategies      []MemoryStrategyDefinition `json:"strategies"`
	EffectivePrompt string                     `json:"effectivePrompt"`
	CurrentModel    string                     `json:"currentModel"`
	Status          MemoryStatus               `json:"status"`
}

type MemoryInput struct {
	Content      string   `json:"content"`
	Topics       []string `json:"topics"`
	Kind         string   `json:"kind"`
	EventTime    string   `json:"eventTime"`
	Participants []string `json:"participants"`
	Location     string   `json:"location"`
}

type MemoryUpdateInput struct {
	ID string `json:"id"`
	MemoryInput
}

type MemoryItem struct {
	ID           string   `json:"id"`
	Content      string   `json:"content"`
	Topics       []string `json:"topics"`
	Kind         string   `json:"kind"`
	EventTime    string   `json:"eventTime"`
	Participants []string `json:"participants"`
	Location     string   `json:"location"`
	CreatedAt    string   `json:"createdAt"`
	UpdatedAt    string   `json:"updatedAt"`
}

type MemoryService struct {
	runtime  *memoryRuntime
	settings *SettingsService
}

func NewMemoryService(runtime *memoryRuntime, settings *SettingsService) *MemoryService {
	return &MemoryService{runtime: runtime, settings: settings}
}

func defaultMemoryConfig() MemoryConfig {
	return MemoryConfig{Enabled: true, AutoExtract: true, Strategy: MemoryStrategyBalanced}
}

func (s *SettingsService) memoryConfig() (MemoryConfig, error) {
	raw, err := s.GetSetting(SettingMemoryConfig)
	if err != nil || strings.TrimSpace(raw) == "" {
		return defaultMemoryConfig(), err
	}
	cfg := defaultMemoryConfig()
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return MemoryConfig{}, fmt.Errorf("读取记忆配置失败: %w", err)
	}
	if _, err := memoryPrompt(cfg.Strategy, cfg.CustomPrompt); err != nil {
		return MemoryConfig{}, err
	}
	return cfg, nil
}

func normalizeMemoryConfig(cfg MemoryConfigInput) (MemoryConfig, error) {
	cfg.Strategy = strings.TrimSpace(cfg.Strategy)
	cfg.CustomPrompt = strings.TrimSpace(cfg.CustomPrompt)
	if _, err := memoryPrompt(cfg.Strategy, cfg.CustomPrompt); err != nil {
		return MemoryConfig{}, err
	}
	return cfg, nil
}

func (s *MemoryService) GetSettings() (MemorySettingsView, error) {
	cfg, err := s.settings.memoryConfig()
	if err != nil {
		return MemorySettingsView{}, err
	}
	prompt, err := memoryPrompt(cfg.Strategy, cfg.CustomPrompt)
	if err != nil {
		return MemorySettingsView{}, err
	}
	modelName := "未配置"
	if p, providerErr := s.settings.DefaultProvider(); providerErr == nil {
		modelName = p.Name + " / " + p.Model
	}
	return MemorySettingsView{Config: cfg, Strategies: memoryStrategies(), EffectivePrompt: prompt, CurrentModel: modelName, Status: s.runtime.snapshotStatus()}, nil
}

func (s *MemoryService) SaveSettings(input MemoryConfigInput) (MemorySettingsView, error) {
	cfg, err := normalizeMemoryConfig(input)
	if err != nil {
		return MemorySettingsView{}, err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return MemorySettingsView{}, err
	}
	if err := s.settings.SetSetting(SettingMemoryConfig, string(raw)); err != nil {
		return MemorySettingsView{}, err
	}
	s.runtime.emit("memory.changed", "settings")
	return s.GetSettings()
}

func memoryItem(entry *memory.Entry) MemoryItem {
	item := MemoryItem{ID: entry.ID, Topics: []string{}, Participants: []string{}, CreatedAt: entry.CreatedAt.Format(time.RFC3339), UpdatedAt: entry.UpdatedAt.Format(time.RFC3339)}
	if entry.Memory == nil {
		return item
	}
	item.Content = entry.Memory.Memory
	item.Topics = append(item.Topics, entry.Memory.Topics...)
	item.Kind = string(entry.Memory.Kind)
	if item.Kind == "" {
		item.Kind = string(memory.KindFact)
	}
	if entry.Memory.EventTime != nil {
		item.EventTime = entry.Memory.EventTime.Format(time.RFC3339)
	}
	item.Participants = append(item.Participants, entry.Memory.Participants...)
	item.Location = entry.Memory.Location
	return item
}

func (s *MemoryService) ListMemories() ([]MemoryItem, error) {
	entries, err := s.runtime.ReadMemories(context.Background(), blankMindMemoryUser, memoryListLimit)
	if err != nil {
		return nil, err
	}
	items := make([]MemoryItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, memoryItem(entry))
	}
	return items, nil
}

func parseMemoryInput(input MemoryInput) (string, []string, *memory.Metadata, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return "", nil, nil, errors.New("记忆内容不能为空")
	}
	kind := memory.Kind(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = memory.KindFact
	}
	if kind != memory.KindFact && kind != memory.KindEpisode {
		return "", nil, nil, errors.New("记忆类型必须是 fact 或 episode")
	}
	metadata := &memory.Metadata{Kind: kind, Participants: input.Participants, Location: strings.TrimSpace(input.Location)}
	if strings.TrimSpace(input.EventTime) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(input.EventTime))
		if err != nil {
			parsed, err = time.Parse(time.DateOnly, strings.TrimSpace(input.EventTime))
		}
		if err != nil {
			return "", nil, nil, errors.New("事件时间格式无效")
		}
		metadata.EventTime = &parsed
	}
	if kind == memory.KindEpisode && metadata.EventTime == nil {
		return "", nil, nil, errors.New("情景记忆必须填写事件时间")
	}
	topics := make([]string, 0, len(input.Topics))
	for _, topic := range input.Topics {
		if topic = strings.TrimSpace(topic); topic != "" {
			topics = append(topics, topic)
		}
	}
	return content, topics, metadata, nil
}

func (s *MemoryService) AddMemory(input MemoryInput) (MemoryItem, error) {
	content, topics, metadata, err := parseMemoryInput(input)
	if err != nil {
		return MemoryItem{}, err
	}
	ctx := context.Background()
	if err := s.runtime.AddMemory(ctx, blankMindMemoryUser, content, topics, memory.WithMetadata(metadata)); err != nil {
		return MemoryItem{}, err
	}
	return s.findByContent(ctx, content)
}

func (s *MemoryService) findByContent(ctx context.Context, content string) (MemoryItem, error) {
	entries, err := s.runtime.ReadMemories(ctx, blankMindMemoryUser, memoryListLimit)
	if err != nil {
		return MemoryItem{}, err
	}
	for _, entry := range entries {
		if entry.Memory != nil && entry.Memory.Memory == content {
			return memoryItem(entry), nil
		}
	}
	return MemoryItem{}, errors.New("记忆写入后未找到")
}

func (s *MemoryService) UpdateMemory(input MemoryUpdateInput) (MemoryItem, error) {
	if strings.TrimSpace(input.ID) == "" {
		return MemoryItem{}, errors.New("记忆 ID 不能为空")
	}
	content, topics, metadata, err := parseMemoryInput(input.MemoryInput)
	if err != nil {
		return MemoryItem{}, err
	}
	result := &memory.UpdateResult{MemoryID: input.ID}
	err = s.runtime.UpdateMemory(context.Background(), memory.Key{AppName: aguiAppName, UserID: aguiUserID, MemoryID: input.ID}, content, topics,
		memory.WithUpdateMetadata(metadata), memory.WithUpdateResult(result))
	if err != nil {
		return MemoryItem{}, err
	}
	return s.findByContent(context.Background(), content)
}

func (s *MemoryService) DeleteMemory(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("记忆 ID 不能为空")
	}
	if err := s.runtime.DeleteMemory(context.Background(), memory.Key{AppName: aguiAppName, UserID: aguiUserID, MemoryID: id}); err != nil {
		return err
	}
	s.runtime.emit("memory.changed", "deleted")
	return nil
}

func (s *MemoryService) ClearMemories() error {
	if err := s.runtime.ClearMemories(context.Background(), blankMindMemoryUser); err != nil {
		return err
	}
	s.runtime.emit("memory.changed", "cleared")
	return nil
}

func (s *MemoryService) ExportMemories() (string, error) {
	items, err := s.ListMemories()
	if err != nil {
		return "", err
	}
	payload := struct {
		SchemaVersion int          `json:"schemaVersion"`
		ExportedAt    string       `json:"exportedAt"`
		Memories      []MemoryItem `json:"memories"`
	}{SchemaVersion: 1, ExportedAt: time.Now().Format(time.RFC3339), Memories: items}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return writeOutput("memories", "memories-"+stamp()+".json", append(raw, '\n'))
}

func (s *MemoryService) ServiceShutdown() error { return s.runtime.Close() }
