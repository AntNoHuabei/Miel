package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	openaiopt "github.com/openai/openai-go/option"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
)

// PingResult 联通测试返回给前端的结构化结果。
type PingResult struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	Model     string `json:"model"`
	LatencyMs int64  `json:"latencyMs"`
}

const providerRequestTimeout = 15 * time.Second

const providerCapabilityCacheTTL = 10 * time.Minute

const chatModelMaxRetries = 3

type providerCapabilityCacheEntry struct {
	expiresAt time.Time
	models    map[string]DiscoveredModel
}

// convProvider 把表单入参转为完整 Provider 结构(补齐空字段)。
func convProvider(in ProviderInput) Provider {
	return Provider{
		ID:         in.ID,
		Name:       in.Name,
		Kind:       in.Kind,
		BaseURL:    in.BaseURL,
		APIKey:     in.APIKey,
		Model:      in.Model,
		Multimodal: in.Multimodal,
		IsDefault:  in.IsDefault,
	}
}

// openaiVariant 把 Kind 映射为 trpc-agent-go openai 变体。
// openai / openrouter / herdsman / volcengine-plan / custom(任意 OpenAI 兼容服务商)
// 与 ollama(v1 兼容端点)
// 走标准 OpenAI 协议。
func openaiVariant(kind string) openai.Variant {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deepseek":
		return openai.VariantDeepSeek
	case "qwen":
		return openai.VariantQwen
	case "hunyuan":
		return openai.VariantHunyuan
	case "glm":
		return openai.VariantGLM
	case "minimax":
		return openai.VariantMiniMax
	case "kimi", "moonshot":
		return openai.VariantKimi
	case "openai", "openrouter", "herdsman", "volcengine-plan", "custom", "ollama":
		return openai.VariantOpenAI
	default:
		return openai.VariantOpenAI
	}
}

// buildModel 把一条 Provider 配置实例化为 trpc-agent-go model.Model。
// 所有内置模板与自定义服务商均走 OpenAI 兼容协议(deepseek/qwen/kimi/ollama 等
// 通过 Variant 或自定义 baseURL 适配),符合框架统一抽象。
func buildModel(p Provider) (model.Model, error) {
	return buildModelWithOptions(p)
}

// buildChatModel keeps retry policy local to conversational model requests.
func buildChatModel(p Provider) (model.Model, error) {
	return buildModelWithOptions(p, openai.WithOpenAIOptions(openaiopt.WithMaxRetries(chatModelMaxRetries)))
}

func buildModelWithOptions(p Provider, extraOptions ...openai.Option) (model.Model, error) {
	name := strings.TrimSpace(p.Name)
	kind := strings.TrimSpace(p.Kind)
	modelName := strings.TrimSpace(p.Model)
	if name == "" || kind == "" || modelName == "" {
		return nil, errors.New("请完整填写服务商名称、类型与模型名")
	}
	opts := []openai.Option{
		openai.WithAPIKey(strings.TrimSpace(p.APIKey)),
		openai.WithVariant(openaiVariant(kind)),
	}
	// DeepSeek defaults to text-only content, so vision models must opt back in.
	if providerSupportsVision(p) {
		opts = append(opts, openai.WithTextOnlyMessageContent(false))
	}
	if base := strings.TrimSpace(p.BaseURL); base != "" {
		opts = append(opts, openai.WithBaseURL(base))
	}
	opts = append(opts, extraOptions...)
	return openai.New(modelName, opts...), nil
}

// providerFromInput 补齐编辑场景中留空的 API Key。
func (s *SettingsService) providerFromInput(in ProviderInput) Provider {
	p := convProvider(in)
	if strings.TrimSpace(p.APIKey) == "" && in.ID > 0 {
		if saved, err := s.getProvider(in.ID); err == nil {
			p.APIKey = saved.APIKey
		}
	}
	return p
}

// modelsEndpoint 将 OpenAI 兼容 Base URL 归一化为模型列表地址。
func modelsEndpoint(base string) (string, error) {
	raw := strings.TrimSpace(base)
	if raw == "" {
		return "", errors.New("API Base URL 不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("API Base URL 格式无效")
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	if !strings.HasSuffix(path, "/models") {
		path += "/models"
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func herdsmanMetadataEndpoint(base string) (string, error) {
	raw := strings.TrimSpace(base)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("API Base URL 格式无效")
	}
	path := strings.TrimRight(u.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	path = strings.TrimSuffix(path, "/models")
	path = strings.TrimSuffix(path, "/v1")
	u.Path = strings.TrimRight(path, "/") + "/api/v1/models"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

type providerModelAPIItem struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Model            string         `json:"model"`
	Status           string         `json:"status"`
	Type             string         `json:"type"`
	Multimodal       bool           `json:"multimodal"`
	ReasoningControl map[string]any `json:"reasoning_control"`
	SupportedParams  []string       `json:"supported_parameters"`
	Architecture     struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
	Parameters struct {
		ReasoningControl map[string]any `json:"reasoning_control"`
	} `json:"parameters"`
}

func fetchProviderModelsBody(p Provider, endpoint string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), providerRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key := strings.TrimSpace(p.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	rsp, err := (&http.Client{Timeout: providerRequestTimeout}).Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("获取模型列表超时(15 秒)")
		}
		return nil, fmt.Errorf("获取模型列表失败: %w", err)
	}
	defer rsp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(rsp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("读取模型列表失败: %w", err)
	}
	if rsp.StatusCode < http.StatusOK || rsp.StatusCode >= http.StatusMultipleChoices {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 300 {
			detail = detail[:300] + "..."
		}
		if detail == "" {
			detail = rsp.Status
		}
		return nil, fmt.Errorf("获取模型列表失败(%d): %s", rsp.StatusCode, detail)
	}

	return body, nil
}

func providerModelID(item providerModelAPIItem) string {
	for _, candidate := range []string{item.ID, item.Name, item.Model} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

func providerReasoningSpec(control map[string]any) ReasoningSpec {
	typ := strings.ToLower(strings.TrimSpace(fmt.Sprint(control["type"])))
	levels := providerReasoningLevels(control["efforts"])
	forced, _ := control["forced_enabled"].(bool)
	locked, _ := control["locked"].(bool)

	switch typ {
	case "effort":
		return ReasoningSpec{Type: ReasoningEffort, Levels: levels}
	case "thinking":
		if forced || locked {
			return ReasoningSpec{Type: ReasoningAlways}
		}
		if len(levels) > 0 {
			return ReasoningSpec{Type: ReasoningEffort, Levels: levels}
		}
		return ReasoningSpec{Type: ReasoningToggle}
	default:
		return ReasoningSpec{Type: ReasoningNone}
	}
}

func providerReasoningLevels(value any) []string {
	var items []any
	switch typed := value.(type) {
	case []any:
		items = typed
	case []string:
		items = make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
	default:
		return nil
	}
	allowed := map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	seen := map[string]bool{}
	levels := make([]string, 0, len(items))
	for _, item := range items {
		level := strings.ToLower(strings.TrimSpace(fmt.Sprint(item)))
		if allowed[level] && !seen[level] {
			seen[level] = true
			levels = append(levels, level)
		}
	}
	return levels
}

func discoveredModelFromAPIItem(item providerModelAPIItem) DiscoveredModel {
	control := item.ReasoningControl
	if control == nil {
		control = item.Parameters.ReasoningControl
	}
	reasoning := providerReasoningSpec(control)
	if reasoning.Type == ReasoningNone && stringSliceContainsFold(item.SupportedParams, "reasoning_effort") {
		reasoning = ReasoningSpec{Type: ReasoningEffort, Levels: []string{"low", "medium", "high", "xhigh"}}
	}
	return DiscoveredModel{
		ID:            providerModelID(item),
		Status:        strings.TrimSpace(item.Status),
		Reasoning:     reasoning,
		Multimodal:    item.Multimodal || strings.EqualFold(strings.TrimSpace(item.Type), "multimodal") || stringSliceContainsFold(item.Architecture.InputModalities, "image"),
		SupportsTools: stringSliceContainsFold(item.SupportedParams, "tools"),
	}
}

func stringSliceContainsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func catalogDiscoveredModels(kind string) ([]DiscoveredModel, bool) {
	provider, ok := catalogLookup(kind)
	if !ok || len(provider.Models) == 0 {
		return nil, false
	}
	models := make([]DiscoveredModel, 0, len(provider.Models))
	for _, item := range provider.Models {
		models = append(models, DiscoveredModel{
			ID:            item.ID,
			Status:        "available",
			Reasoning:     item.Reasoning,
			Multimodal:    item.Multimodal,
			SupportsTools: true,
		})
	}
	return models, true
}

// DiscoverProviderModels 从 OpenAI 兼容 /models 读取模型，并为 Herdsman 合并
// 本地模型元数据中的动态推理与多模态能力。Agent Plan 没有模型发现
// 接口，直接返回随应用维护的官方文本模型目录。
func (s *SettingsService) DiscoverProviderModels(in ProviderInput) ([]DiscoveredModel, error) {
	p := s.providerFromInput(in)
	if strings.EqualFold(strings.TrimSpace(p.Kind), "volcengine-plan") {
		if models, ok := catalogDiscoveredModels(p.Kind); ok {
			return models, nil
		}
		return nil, errors.New("Agent Plan 内置模型目录不可用")
	}
	endpoint, err := modelsEndpoint(p.BaseURL)
	if err != nil {
		return nil, err
	}
	body, err := fetchProviderModelsBody(p, endpoint)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data   []providerModelAPIItem `json:"data"`
		Models []providerModelAPIItem `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("模型列表响应格式无效: %w", err)
	}

	byID := make(map[string]DiscoveredModel, len(payload.Data)+len(payload.Models))
	add := func(item providerModelAPIItem) {
		model := discoveredModelFromAPIItem(item)
		if model.ID == "" {
			return
		}
		if _, ok := byID[model.ID]; ok {
			return
		}
		byID[model.ID] = model
	}
	for _, item := range payload.Data {
		add(item)
	}
	for _, item := range payload.Models {
		add(item)
	}
	if len(byID) == 0 {
		return nil, errors.New("服务商返回了空模型列表")
	}

	if strings.EqualFold(strings.TrimSpace(p.Kind), "herdsman") {
		if metadataURL, metadataErr := herdsmanMetadataEndpoint(p.BaseURL); metadataErr == nil {
			if metadataBody, fetchErr := fetchProviderModelsBody(p, metadataURL); fetchErr == nil {
				var metadata []providerModelAPIItem
				if json.Unmarshal(metadataBody, &metadata) == nil {
					chatModels := make(map[string]bool, len(metadata))
					for _, item := range metadata {
						id := providerModelID(item)
						if _, exists := byID[id]; !exists {
							continue
						}
						modelType := strings.ToLower(strings.TrimSpace(item.Type))
						if modelType != "text-generation" && modelType != "multimodal" {
							continue
						}
						chatModels[id] = true
						model := discoveredModelFromAPIItem(item)
						model.Status = byID[id].Status
						byID[id] = model
					}
					for id := range byID {
						if !chatModels[id] {
							delete(byID, id)
						}
					}
				}
			}
		}
	}

	models := make([]DiscoveredModel, 0, len(byID))
	for _, item := range byID {
		models = append(models, item)
	}
	if len(models) == 0 {
		return nil, errors.New("服务商未返回可用聊天模型")
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	s.cacheProviderCapabilities(p, models)
	return models, nil
}

func providerCapabilityCacheKey(p Provider) string {
	return fmt.Sprintf("%d|%s|%s", p.ID, strings.ToLower(strings.TrimSpace(p.Kind)), strings.TrimRight(strings.TrimSpace(p.BaseURL), "/"))
}

func (s *SettingsService) cacheProviderCapabilities(p Provider, models []DiscoveredModel) {
	if s == nil || !strings.EqualFold(strings.TrimSpace(p.Kind), "openrouter") {
		return
	}
	byID := make(map[string]DiscoveredModel, len(models))
	for _, item := range models {
		byID[strings.ToLower(strings.TrimSpace(item.ID))] = item
	}
	s.capabilityMu.Lock()
	defer s.capabilityMu.Unlock()
	if s.capabilityCache == nil {
		s.capabilityCache = make(map[string]providerCapabilityCacheEntry)
	}
	s.capabilityCache[providerCapabilityCacheKey(p)] = providerCapabilityCacheEntry{
		expiresAt: time.Now().Add(providerCapabilityCacheTTL),
		models:    byID,
	}
}

func (s *SettingsService) cachedProviderCapability(p Provider, modelName string) (DiscoveredModel, bool) {
	if s == nil {
		return DiscoveredModel{}, false
	}
	key := providerCapabilityCacheKey(p)
	s.capabilityMu.RLock()
	entry, ok := s.capabilityCache[key]
	s.capabilityMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			s.capabilityMu.Lock()
			delete(s.capabilityCache, key)
			s.capabilityMu.Unlock()
		}
		return DiscoveredModel{}, false
	}
	item, ok := entry.models[strings.ToLower(strings.TrimSpace(modelName))]
	return item, ok
}

func (s *SettingsService) invalidateProviderCapabilities(providerID int64) {
	if s == nil {
		return
	}
	prefix := fmt.Sprintf("%d|", providerID)
	s.capabilityMu.Lock()
	defer s.capabilityMu.Unlock()
	for key := range s.capabilityCache {
		if strings.HasPrefix(key, prefix) {
			delete(s.capabilityCache, key)
		}
	}
}

// providerSupportsTools returns known=false when OpenRouter capability discovery
// is unavailable. Callers fail open in that case to preserve the full Agent.
func (s *SettingsService) providerSupportsTools(p Provider) (supported, known bool) {
	if !strings.EqualFold(strings.TrimSpace(p.Kind), "openrouter") {
		return true, true
	}
	if item, ok := s.cachedProviderCapability(p, p.Model); ok {
		return item.SupportsTools, true
	}
	models, err := s.DiscoverProviderModels(ProviderInput{
		ID: p.ID, Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL,
		APIKey: p.APIKey, Model: p.Model,
	})
	if err != nil {
		return true, false
	}
	for _, item := range models {
		if strings.EqualFold(strings.TrimSpace(item.ID), strings.TrimSpace(p.Model)) {
			return item.SupportsTools, true
		}
	}
	return true, false
}

// FetchProviderModels 保留原有字符串列表接口，供旧调用方兼容使用。
func (s *SettingsService) FetchProviderModels(in ProviderInput) ([]string, error) {
	discovered, err := s.DiscoverProviderModels(in)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(discovered))
	for _, item := range discovered {
		models = append(models, item.ID)
	}
	return models, nil
}

// PingProvider 向服务商发送一次最小对话,验证 baseURL / apiKey / model 是否可用。
// 它是“模型配置向导”的联通测试入口;引导完成后即进入对话 Agent 阶段。
func (s *SettingsService) PingProvider(in ProviderInput) (PingResult, error) {
	m, err := buildModel(s.providerFromInput(in))
	if err != nil {
		return PingResult{OK: false, Message: err.Error()}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := model.NewRequest([]model.Message{
		model.NewUserMessage("ping,reply ok only"),
	})
	ch, err := m.GenerateContent(ctx, req)
	if err != nil {
		return PingResult{OK: false, Message: "连接失败:" + err.Error()}, nil
	}
	start := time.Now()
	var text strings.Builder
	for rsp := range ch {
		if rsp.Error != nil {
			return PingResult{OK: false, Message: "服务商返回错误:" + fmt.Sprintf("%v", rsp.Error)}, nil
		}
		for _, c := range rsp.Choices {
			if c.Delta.Content != "" {
				text.WriteString(c.Delta.Content)
			}
			if c.Message.Content != "" {
				text.WriteString(c.Message.Content)
			}
		}
	}
	if text.Len() == 0 {
		return PingResult{OK: false, Message: "未收到有效回复(模型可能不支持该请求)"}, nil
	}
	body := strings.TrimSpace(text.String())
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	return PingResult{
		OK:        true,
		Message:   body,
		Model:     strings.TrimSpace(in.Model),
		LatencyMs: time.Since(start).Milliseconds(),
	}, nil
}

// DefaultProvider 返回当前默认服务商;无配置时返回错误(首启向导据此拦截入口)。
func (s *SettingsService) DefaultProvider() (Provider, error) {
	var p Provider
	var mm, def int
	err := s.db.QueryRow(`
		SELECT id, name, kind, base_url, api_key, model, multimodal, is_default, created_at
		FROM providers WHERE is_default = 1 OR id = (SELECT MIN(id) FROM providers)
		ORDER BY is_default DESC LIMIT 1`).
		Scan(&p.ID, &p.Name, &p.Kind, &p.BaseURL, &p.APIKey, &p.Model, &mm, &def, &p.CreatedAt)
	if err != nil {
		return p, errors.New("尚未配置任何模型服务商")
	}
	p.Multimodal = mm != 0
	p.IsDefault = def != 0
	if resolveSecret(&p) {
		_, _ = s.db.Exec("UPDATE providers SET api_key = '' WHERE id = ?", p.ID)
	}
	return p, nil
}

// DefaultModelSupportsVision 按当前默认模型及目录能力判断是否支持图片输入。
func (s *SettingsService) DefaultModelSupportsVision() (bool, error) {
	p, err := s.DefaultProvider()
	if err != nil {
		return false, err
	}
	return providerSupportsVision(p), nil
}
