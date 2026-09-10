package app

import (
	"errors"
	"strings"
)

const (
	MemoryStrategyExplicit      = "explicit"
	MemoryStrategyBalanced      = "balanced"
	MemoryStrategyComprehensive = "comprehensive"
	MemoryStrategyCustom        = "custom"
	maxCustomMemoryPromptRunes  = 20000
)

type MemoryStrategyDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
	Prompt      string `json:"prompt"`
}

const explicitMemoryPrompt = `You manage long-term memory for BlankMind. Today's date is {current_date}.
Extract a memory only when the user explicitly asks you to remember, save, retain, correct, or update something for future conversations.
Do not infer an implicit request. Ignore casual conversation, one-off tasks, model guesses, assistant claims, tool output, credentials, access tokens, private keys, passwords, financial secrets, and other sensitive secrets.
Keep each memory concise, factual, and attributable to the user. Use fact for stable information and episode for a dated event. Prefer updating a matching memory over adding a duplicate.`

const balancedMemoryPrompt = `You manage long-term memory for BlankMind. Today's date is {current_date}.
Extract only durable information that will improve future conversations: stable identity, long-term preferences, ongoing work context, durable relationships, and anything the user explicitly asks to remember or correct.
Do not store casual chat, temporary instructions, isolated task details, model guesses, assistant claims, tool output, credentials, access tokens, private keys, passwords, financial secrets, or other sensitive secrets.
Keep memories atomic, concise, factual, and attributable to the user. Use fact for stable information and episode only for a meaningful dated event. Check existing memories first and prefer updating a matching memory over adding a near-duplicate.`

const comprehensiveMemoryPrompt = `You manage long-term memory for BlankMind. Today's date is {current_date}.
Extract durable facts, preferences, recurring work context, meaningful relationships, decisions, and useful episodes that may improve future conversations, including information not phrased as an explicit memory request.
Be comprehensive but do not store model guesses, assistant claims, tool output, credentials, access tokens, private keys, passwords, financial secrets, medical secrets, intimate secrets, or other sensitive private information.
Keep memories atomic, concise, factual, and attributable to the user. Use fact for stable information and episode for a specific event with an absolute event time. Check existing memories first and prefer updating a matching memory over adding a near-duplicate.`

var builtinMemoryStrategies = []MemoryStrategyDefinition{
	{ID: MemoryStrategyExplicit, Name: "仅明确要求", Description: "只保存用户明确要求记住或修正的信息", Risk: "low", Prompt: explicitMemoryPrompt},
	{ID: MemoryStrategyBalanced, Name: "均衡", Description: "保存稳定偏好、身份和长期工作背景", Risk: "normal", Prompt: balancedMemoryPrompt},
	{ID: MemoryStrategyComprehensive, Name: "全面", Description: "保存更多长期事实和重要经历", Risk: "high", Prompt: comprehensiveMemoryPrompt},
}

func memoryStrategies() []MemoryStrategyDefinition {
	items := make([]MemoryStrategyDefinition, len(builtinMemoryStrategies))
	copy(items, builtinMemoryStrategies)
	return items
}

func memoryPrompt(strategy, custom string) (string, error) {
	strategy = strings.TrimSpace(strategy)
	if strategy == MemoryStrategyCustom {
		custom = strings.TrimSpace(custom)
		if custom == "" {
			return "", errors.New("自定义记忆提示词不能为空")
		}
		if len([]rune(custom)) > maxCustomMemoryPromptRunes {
			return "", errors.New("自定义记忆提示词不能超过 20000 个字符")
		}
		return custom, nil
	}
	for _, item := range builtinMemoryStrategies {
		if item.ID == strategy {
			return item.Prompt, nil
		}
	}
	return "", errors.New("无效的记忆提取策略")
}
