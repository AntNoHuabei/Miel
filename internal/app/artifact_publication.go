package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/yuin/goldmark"
	goldmarkast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"trpc.group/trpc-go/trpc-agent-go/artifact"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

type artifactScopeKey struct{}
type artifactPublisherKey struct{}
type artifactScope struct {
	ConversationID int64
	RequestID      string
	UserMessageID  int64
}

type runArtifactService struct {
	*ArtifactService
	scope artifactScope
}

func (s *runArtifactService) scoped(ctx context.Context) context.Context {
	return context.WithValue(ctx, artifactScopeKey{}, s.scope)
}
func (s *runArtifactService) info() artifact.SessionInfo {
	return artifact.SessionInfo{AppName: aguiAppName, UserID: aguiUserID, SessionID: "conv-" + strconv.FormatInt(s.scope.ConversationID, 10)}
}
func (s *runArtifactService) SaveArtifact(ctx context.Context, info artifact.SessionInfo, name string, data *artifact.Artifact) (int, error) {
	return s.ArtifactService.SaveArtifact(s.scoped(ctx), info, name, data)
}

type artifactOutput struct {
	Path      string        `json:"path"`
	Artifacts []ArtifactRef `json:"artifacts"`
}

func publishOfficeOutput(ctx context.Context, sub, name string, content []byte) (artifactOutput, error) {
	publisher, _ := ctx.Value(artifactPublisherKey{}).(*runArtifactService)
	if publisher == nil {
		path, err := writeOutput(sub, name, content)
		return artifactOutput{Path: path}, err
	}
	ref, path, err := publisher.save(publisher.scoped(ctx), publisher.info(), name, "", bytes.NewReader(content), artifactByteLimit)
	if err != nil {
		return artifactOutput{}, err
	}
	return artifactOutput{Path: path, Artifacts: []ArtifactRef{ref}}, nil
}

type artifactTool struct {
	tool.CallableTool
	publication *runArtifactService
}

func (t *artifactTool) Call(ctx context.Context, args []byte) (any, error) {
	return t.CallableTool.Call(context.WithValue(ctx, artifactPublisherKey{}, t.publication), args)
}

func (t *artifactTool) StateDelta(toolCallID string, _ []byte, resultJSON []byte) map[string][]byte {
	var output artifactOutput
	if t.Declaration().Name == "skill_run" {
		var result skillRunResponse
		if json.Unmarshal(resultJSON, &result) != nil || result.ExitCode != 0 {
			return nil
		}
		var envelope struct {
			OK   bool           `json:"ok"`
			Data artifactOutput `json:"data"`
		}
		if json.Unmarshal([]byte(result.Stdout), &envelope) != nil || !envelope.OK {
			return nil
		}
		output = envelope.Data
	} else {
		if json.Unmarshal(resultJSON, &output) != nil {
			return nil
		}
	}
	if len(output.Artifacts) == 0 || toolCallID == "" || t.publication == nil {
		return nil
	}
	type frameworkRef struct {
		Name    string `json:"name"`
		Version int    `json:"version"`
		Ref     string `json:"ref"`
	}
	refs := []frameworkRef{}
	for _, ref := range output.Artifacts {
		_, err := t.publication.db.Exec(`UPDATE artifact_links SET tool_call_id=? WHERE artifact_id=? AND version=? AND conversation_id=? AND request_id=? AND user_message_id=?`, toolCallID, ref.ID, ref.Version, t.publication.scope.ConversationID, t.publication.scope.RequestID, t.publication.scope.UserMessageID)
		if err != nil {
			continue
		}
		refs = append(refs, frameworkRef{Name: ref.Name, Version: ref.Version, Ref: fmt.Sprintf("artifact://%s@%d", ref.Name, ref.Version)})
	}
	if len(refs) == 0 {
		return nil
	}
	data, err := json.Marshal(struct {
		ToolCallID string         `json:"tool_call_id"`
		Artifacts  []frameworkRef `json:"artifacts"`
	}{toolCallID, refs})
	if err != nil {
		return nil
	}
	return map[string][]byte{skill.StateKeyArtifacts: data}
}

func newPublishArtifactTool(env permissionToolEnv, publisher *runArtifactService) tool.Tool {
	base := function.NewFunctionTool(func(ctx context.Context, in filePathInput) (artifactOutput, error) {
		path, outside, err := env.resolvePath(in.Path)
		if err != nil {
			return artifactOutput{}, err
		}
		if result, ok := env.authorize(ctx, "publish_artifact", "read", path, path, outside); !ok {
			return artifactOutput{}, errors.New(result.Message)
		}
		// Revalidate after approval: a selected path may have changed while waiting.
		current, currentOutside, err := env.resolvePath(in.Path)
		if err != nil || current != path || currentOutside != outside {
			return artifactOutput{}, errors.New("文件路径在批准后发生变化")
		}
		ref, err := publisher.importFile(publisher.scoped(ctx), publisher.info(), path)
		if err != nil {
			return artifactOutput{}, err
		}
		_, managed, err := publisher.refPath(ref.ID, ref.Version)
		return artifactOutput{Path: managed, Artifacts: []ArtifactRef{ref}}, err
	}, function.WithName("publish_artifact"), function.WithDescription("发布明确生成的最终交付文件到本轮对话。复制为不可变产物，不移动原件；读取由应用权限控制。不要发布普通编辑、日志或临时文件。"))
	return &artifactTool{CallableTool: base, publication: publisher}
}

func (s *ArtifactService) linkRefs(conversationID int64, requestID, toolCallID string) ([]ArtifactRef, error) {
	rows, err := s.db.Query(`SELECT artifact_id,version FROM artifact_links WHERE conversation_id=? AND request_id=? AND (?='' OR tool_call_id=?) ORDER BY rowid`, conversationID, requestID, toolCallID, toolCallID)
	if err != nil {
		return nil, err
	}
	type key struct {
		id string
		v  int
	}
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.v); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	refs := []ArtifactRef{}
	for _, k := range keys {
		ref, err := s.Get(k.id, k.v)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (s *ArtifactService) finishRun(scope artifactScope, messageID string) error {
	_, err := s.db.Exec("UPDATE artifact_links SET assistant_message_id=? WHERE conversation_id=? AND request_id=? AND user_message_id=?", messageID, scope.ConversationID, scope.RequestID, scope.UserMessageID)
	return err
}

func (s *ArtifactService) attachSnapshot(snapshot map[string]any, conversationID int64) error {
	rows, err := s.db.Query(`SELECT artifact_id,version,user_message_id,request_id,assistant_message_id FROM artifact_links WHERE conversation_id=? ORDER BY rowid`, conversationID)
	if err != nil {
		return err
	}
	type link struct {
		id                 string
		version            int
		user               int64
		request, assistant string
	}
	var links []link
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.id, &l.version, &l.user, &l.request, &l.assistant); err != nil {
			rows.Close()
			return err
		}
		links = append(links, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	messages, _ := snapshot["messages"].([]any)
	byID := map[string]map[string]any{}
	for _, raw := range messages {
		if m, ok := raw.(map[string]any); ok {
			if id, ok := m["id"].(string); ok {
				byID[id] = m
			}
		}
	}
	orphans := map[int64][]any{}
	orphanByRun := map[string]map[string]any{}
	for _, l := range links {
		ref, err := s.Get(l.id, l.version)
		if err != nil {
			return err
		}
		target := byID[l.assistant]
		if target == nil || target["role"] != "assistant" {
			key := strconv.FormatInt(l.user, 10) + ":" + l.request
			target = orphanByRun[key]
			if target == nil {
				target = map[string]any{"id": "artifacts-" + key, "role": "artifact", "artifacts": []ArtifactRef{}}
				orphanByRun[key] = target
				orphans[l.user] = append(orphans[l.user], target)
			}
		}
		refs, _ := target["artifacts"].([]ArtifactRef)
		target["artifacts"] = append(refs, ref)
	}
	merged := []any{}
	var pending []any
	for _, raw := range messages {
		m, _ := raw.(map[string]any)
		if m["role"] == "user" {
			merged = append(merged, pending...)
			pending = nil
			id, _ := m["id"].(string)
			user, _ := strconv.ParseInt(stringsTrimMessagePrefix(id), 10, 64)
			pending = orphans[user]
			delete(orphans, user)
		}
		merged = append(merged, raw)
	}
	merged = append(merged, pending...)
	for _, entries := range orphans {
		merged = append(merged, entries...)
	}
	snapshot["messages"] = merged
	return nil
}

func stringsTrimMessagePrefix(id string) string {
	if len(id) > 0 && id[0] == 'm' {
		return id[1:]
	}
	return id
}

type SaveMessageArtifactRequest struct {
	ConversationID int64  `json:"conversationId"`
	MessageID      string `json:"messageId"`
	Name           string `json:"name"`
	CodeBlock      *int   `json:"codeBlock,omitempty"`
}

func (s *ArtifactService) SaveMessage(request SaveMessageArtifactRequest) (ArtifactRef, error) {
	if s.agent == nil {
		return ArtifactRef{}, errors.New("对话服务未初始化")
	}
	snapshot, err := s.agent.MessagesSnapshot(request.ConversationID)
	if err != nil {
		return ArtifactRef{}, err
	}
	messages, _ := snapshot["messages"].([]any)
	var content string
	found := false
	for _, raw := range messages {
		m, _ := raw.(map[string]any)
		if m["id"] == request.MessageID && m["role"] == "assistant" {
			content, _ = m["content"].(string)
			found = true
			break
		}
	}
	if !found {
		return ArtifactRef{}, errors.New("助手消息不存在")
	}
	if request.CodeBlock != nil {
		blocks := markdownCodeBlocks(content)
		if *request.CodeBlock < 0 || *request.CodeBlock >= len(blocks) {
			return ArtifactRef{}, errors.New("代码块不存在")
		}
		content = blocks[*request.CodeBlock]
	}
	if len(content) == 0 {
		return ArtifactRef{}, errors.New("消息内容为空")
	}
	scope := artifactScope{ConversationID: request.ConversationID, RequestID: "manual-" + request.MessageID}
	ctx := context.WithValue(context.Background(), artifactScopeKey{}, scope)
	ref, _, err := s.save(ctx, artifact.SessionInfo{AppName: aguiAppName, UserID: aguiUserID, SessionID: "conv-" + strconv.FormatInt(request.ConversationID, 10)}, request.Name, "", bytes.NewReader([]byte(content)), artifactByteLimit)
	if err != nil {
		return ArtifactRef{}, err
	}
	_, err = s.db.Exec("UPDATE artifact_links SET assistant_message_id=? WHERE artifact_id=? AND version=? AND conversation_id=?", request.MessageID, ref.ID, ref.Version, request.ConversationID)
	return ref, err
}

// markdownCodeBlocks uses the same CommonMark parser as the model context renderer.
func markdownCodeBlocks(content string) []string { return extractArtifactCodeBlocks(content) }

func extractArtifactCodeBlocks(content string) []string {
	source := []byte(content)
	document := goldmark.DefaultParser().Parse(text.NewReader(source))
	var blocks []string
	_ = goldmarkast.Walk(document, func(node goldmarkast.Node, entering bool) (goldmarkast.WalkStatus, error) {
		if !entering || (node.Kind() != goldmarkast.KindFencedCodeBlock && node.Kind() != goldmarkast.KindCodeBlock) {
			return goldmarkast.WalkContinue, nil
		}
		var block bytes.Buffer
		lines := node.Lines()
		for index := 0; index < lines.Len(); index++ {
			segment := lines.At(index)
			block.Write(segment.Value(source))
		}
		blocks = append(blocks, block.String())
		return goldmarkast.WalkSkipChildren, nil
	})
	return blocks
}
