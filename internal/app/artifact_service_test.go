package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/artifact"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

func newArtifactTestService(t *testing.T) *ArtifactService {
	t.Helper()
	service, err := NewArtifactService(newTodoTestDB(t), NewDirectoryManager(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func testArtifactSession() artifact.SessionInfo {
	return artifact.SessionInfo{AppName: "blankmind-app", UserID: "user", SessionID: "conv-7"}
}

func TestArtifactServiceVersionsAndPersistsAcrossRestart(t *testing.T) {
	s := newArtifactTestService(t)
	for index, data := range [][]byte{[]byte("one"), []byte("two")} {
		version, err := s.SaveArtifact(context.Background(), testArtifactSession(), "notes.md", &artifact.Artifact{Data: data, MimeType: "text/markdown"})
		if err != nil || version != index {
			t.Fatalf("save version = %d, %v", version, err)
		}
	}
	loaded, err := s.LoadArtifact(context.Background(), testArtifactSession(), "notes.md", nil)
	if err != nil || string(loaded.Data) != "two" {
		t.Fatalf("latest = %#v, %v", loaded, err)
	}
	restarted, err := NewArtifactService(s.db, s.dirs)
	if err != nil {
		t.Fatal(err)
	}
	versions, err := restarted.ListVersions(context.Background(), testArtifactSession(), "notes.md")
	if err != nil || len(versions) != 2 || versions[0] != 0 || versions[1] != 1 {
		t.Fatalf("versions = %#v, %v", versions, err)
	}
}

func TestArtifactServiceRejectsOversizeAndCleansPendingFiles(t *testing.T) {
	s := newArtifactTestService(t)
	_, _, err := s.save(context.Background(), testArtifactSession(), "large.bin", "application/octet-stream", bytes.NewReader(make([]byte, 17)), 16)
	if !errors.Is(err, ErrArtifactTooLarge) {
		t.Fatalf("error = %v", err)
	}
	entries, err := os.ReadDir(s.dirs.Path(DirectoryArtifactFiles))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial files remain: %#v", entries)
	}
}

func TestArtifactServiceRangeAndTokenProtection(t *testing.T) {
	s := newArtifactTestService(t)
	version, err := s.SaveArtifact(context.Background(), testArtifactSession(), "clip.mp4", &artifact.Artifact{Data: []byte("0123456789"), MimeType: "video/mp4"})
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err := s.db.QueryRow("SELECT id FROM artifacts").Scan(&id); err != nil {
		t.Fatal(err)
	}
	unauthorized := httptest.NewRecorder()
	s.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/artifacts/"+id+"/0", nil))
	if unauthorized.Code != http.StatusForbidden {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/artifacts/"+id+"/"+string(rune('0'+version))+"?token="+s.token, nil)
	req.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	s.ServeHTTP(response, req)
	if response.Code != http.StatusPartialContent || response.Body.String() != "2345" {
		t.Fatalf("range = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Security-Policy") != "" || response.Header().Get("Referrer-Policy") != "" {
		t.Fatalf("artifact resource unexpectedly restricted: %#v", response.Header())
	}
}

func TestArtifactDeleteRestoreAndConversationUnlink(t *testing.T) {
	s := newArtifactTestService(t)
	ctx := context.WithValue(context.Background(), artifactScopeKey{}, artifactScope{ConversationID: 7, RequestID: "r", UserMessageID: 9})
	ref, _, err := s.save(ctx, testArtifactSession(), "a.txt", "text/plain", bytes.NewBufferString("data"), artifactByteLimit)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ref.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.Get(ref.ID, 0)
	if err != nil || deleted.Availability != "deleted" {
		t.Fatalf("deleted = %#v %v", deleted, err)
	}
	if err := s.Restore(ref.ID); err != nil {
		t.Fatal(err)
	}
	restored, err := s.Get(ref.ID, 0)
	if err != nil || restored.Availability != "available" {
		t.Fatalf("restored = %#v %v", restored, err)
	}
	if _, err := s.db.Exec("INSERT INTO conversations(id,title,created_at,updated_at) VALUES(7,'x',1,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec("INSERT INTO messages(id,conversation_id,role,content,created_at) VALUES(9,7,'user','x',1)"); err != nil {
		t.Fatal(err)
	}
	previousStore := store
	store = s.db
	defer func() { store = previousStore }()
	agent := &AgentService{artifacts: s}
	if err := agent.DeleteConversation(7); err != nil {
		t.Fatal(err)
	}
	var links int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM artifact_links WHERE artifact_id=?", ref.ID).Scan(&links); err != nil || links != 0 {
		t.Fatalf("links = %d %v", links, err)
	}
	if _, err := os.Stat(filepath.Join(s.dirs.Path(DirectoryArtifactFiles), mustBlobName(t, s, ref.ID))); err != nil {
		t.Fatal(err)
	}
}

func mustBlobName(t *testing.T, s *ArtifactService, id string) string {
	t.Helper()
	var name string
	if err := s.db.QueryRow("SELECT blob_name FROM artifact_versions WHERE artifact_id=?", id).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func TestArtifactImportRejectsDirectoriesAndSymlinkEscape(t *testing.T) {
	s := newArtifactTestService(t)
	if _, err := s.importFile(context.Background(), testArtifactSession(), t.TempDir()); err == nil {
		t.Fatal("directory import succeeded")
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(s.dirs.Path(DirectoryArtifactFiles), "escape.blob")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := s.db.Exec("INSERT INTO artifacts(id,app_name,user_id,session_id,name,created_at) VALUES('escape','a','u','s','x',1)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec("INSERT INTO artifact_versions(artifact_id,version,mime_type,kind,size,blob_name,created_at) VALUES('escape',0,'text/plain','text',6,'escape.blob',1)")
	if err != nil {
		t.Fatal(err)
	}
	ref, _, err := s.refPath("escape", 0)
	if err != nil || ref.Availability != "missing" {
		t.Fatalf("escaped ref = %#v %v", ref, err)
	}
}

func TestArtifactToolEmitsFrameworkDeltaAndSnapshotAssociatesArtifacts(t *testing.T) {
	s := newArtifactTestService(t)
	publication := &runArtifactService{ArtifactService: s, scope: artifactScope{ConversationID: 7, RequestID: "r1", UserMessageID: 9}}
	ctx := context.WithValue(context.Background(), artifactPublisherKey{}, publication)
	output, err := publishOfficeOutput(ctx, "documents", "report.md", []byte("# report"))
	if err != nil {
		t.Fatal(err)
	}
	result := skillRunResponse{ExitCode: 0}
	encodedEnvelope, _ := json.Marshal(skillCommandEnvelope{OK: true, Data: output})
	result.Stdout = string(encodedEnvelope)
	resultJSON, _ := json.Marshal(result)
	wrapped := &artifactTool{CallableTool: newSkillRunTool(nil, nil).(tool.CallableTool), publication: publication}
	delta := wrapped.StateDelta("tool-1", nil, resultJSON)
	if len(delta[skill.StateKeyArtifacts]) == 0 {
		t.Fatal("artifact state delta missing")
	}
	if err := s.finishRun(publication.scope, "assistant-1"); err != nil {
		t.Fatal(err)
	}
	snapshot := map[string]any{"messages": []any{map[string]any{"id": "m9", "role": "user", "content": "make report"}, map[string]any{"id": "assistant-1", "role": "assistant", "content": "done"}}}
	if err := s.attachSnapshot(snapshot, 7); err != nil {
		t.Fatal(err)
	}
	messages := snapshot["messages"].([]any)
	assistant := messages[1].(map[string]any)
	refs := assistant["artifacts"].([]ArtifactRef)
	if len(refs) != 1 || refs[0].ID != output.Artifacts[0].ID {
		t.Fatalf("snapshot refs = %#v", refs)
	}

	publication.scope = artifactScope{ConversationID: 7, RequestID: "r2", UserMessageID: 9}
	ctx = context.WithValue(context.Background(), artifactPublisherKey{}, publication)
	if _, err := publishOfficeOutput(ctx, "documents", "orphan.md", []byte("orphan")); err != nil {
		t.Fatal(err)
	}
	snapshot = map[string]any{"messages": []any{map[string]any{"id": "m9", "role": "user", "content": "make report"}}}
	if err := s.attachSnapshot(snapshot, 7); err != nil {
		t.Fatal(err)
	}
	messages = snapshot["messages"].([]any)
	if len(messages) != 3 || messages[2].(map[string]any)["role"] != "artifact" {
		t.Fatalf("orphan snapshot = %#v", messages)
	}
}

func TestArtifactToolAutomaticallyPublishesVerifiedSkillOutput(t *testing.T) {
	s := newArtifactTestService(t)
	publication := &runArtifactService{ArtifactService: s, scope: artifactScope{ConversationID: 7, RequestID: "r1", UserMessageID: 9}}
	output := filepath.Join(t.TempDir(), "watermarked.jpg")
	if err := os.WriteFile(output, []byte("watermarked-image"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := function.NewFunctionTool(func(context.Context, struct{}) (skillRunResponse, error) {
		return skillRunResponse{ExitCode: 0, OutputPaths: []string{output}}, nil
	}, function.WithName("skill_run"))
	wrapped := &artifactTool{CallableTool: base, publication: publication}
	result, err := wrapped.Call(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	run, ok := result.(skillRunResponse)
	if !ok || len(run.Artifacts) != 1 || run.Artifacts[0].Name != "watermarked.jpg" {
		t.Fatalf("automatic publication = %#v", result)
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	if delta := wrapped.StateDelta("tool-1", nil, encoded); len(delta[skill.StateKeyArtifacts]) == 0 {
		t.Fatal("automatic publication state delta missing")
	}
}

func TestExtractArtifactCodeBlocksUsesCommonMarkFences(t *testing.T) {
	blocks := extractArtifactCodeBlocks("before\n~~~go\nfmt.Println(1)\n~~~\n\n    indented()\n")
	if len(blocks) != 2 || blocks[0] != "fmt.Println(1)\n" || blocks[1] != "indented()\n" {
		t.Fatalf("blocks = %#v", blocks)
	}
}
