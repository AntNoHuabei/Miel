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
// openai / custom(任意 OpenAI 兼容服务商)与 ollama(v1 兼容端点)走标准 OpenAI 协议。
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
	default:
		return openai.VariantOpenAI
	}
}

// buildModel 把一条 Provider 配置实例化为 trpc-agent-go model.Model。
// 所有内置模板与自定义服务商均走 OpenAI 兼容协议(deepseek/qwen/kimi/ollama 等
// 通过 Variant 或自定义 baseURL 适配),符合框架统一抽象。
func buildModel(p Provider) (model.Model, error) {
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

// FetchProviderModels 从服务商的 OpenAI 兼容 /models 端点读取可用模型。
func (s *SettingsService) FetchProviderModels(in ProviderInput) ([]string, error) {
	p := s.providerFromInput(in)
	endpoint, err := modelsEndpoint(p.BaseURL)
	if err != nil {
		return nil, err
	}
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

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("模型列表响应格式无效: %w", err)
	}
	seen := make(map[string]struct{}, len(payload.Data)+len(payload.Models))
	models := make([]string, 0, len(payload.Data)+len(payload.Models))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		models = append(models, name)
	}
	for _, item := range payload.Data {
		add(item.ID)
	}
	for _, item := range payload.Models {
		name := item.ID
		if name == "" {
			name = item.Name
		}
		if name == "" {
			name = item.Model
		}
		add(name)
	}
	if len(models) == 0 {
		return nil, errors.New("服务商返回了空模型列表")
	}
	sort.Strings(models)
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
