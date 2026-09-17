package app

import (
	"context"
	"testing"
)

func setupPlanTestStore(t *testing.T) *AgentService {
	t.Helper()
	db := newSettingsTestDB(t)
	oldStore, oldSettings := store, settingsSvc
	store = db
	settingsSvc = NewSettingsService(db)
	t.Cleanup(func() {
		store = oldStore
		settingsSvc = oldSettings
	})
	if _, err := db.Exec(`
		INSERT INTO providers (id, name, kind, base_url, api_key, model, multimodal, is_default, created_at)
		VALUES (1, 'One', 'custom', 'http://one.invalid/v1', '', 'model-a', 0, 1, 1),
		       (2, 'Two', 'custom', 'http://two.invalid/v1', '', 'model-b', 0, 0, 2);
		INSERT INTO provider_models (provider_id, model, label, custom, multimodal, created_at)
		VALUES (1, 'model-a', 'A', 1, 0, 1), (2, 'model-b', 'B', 1, 0, 2);
		INSERT INTO conversations (id, title, provider_id, model, created_at, updated_at)
		VALUES (10, 'first', 1, 'model-a', 1, 1), (11, 'second', 1, 'model-a', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	return &AgentService{}
}

func TestConversationModelsAreIndependent(t *testing.T) {
	service := setupPlanTestStore(t)
	if err := service.SetConversationModel(SetConversationModelRequest{ConversationID: 10, ProviderID: 2, Model: "model-b"}); err != nil {
		t.Fatal(err)
	}
	first, err := providerForConversation(10)
	if err != nil {
		t.Fatal(err)
	}
	second, err := providerForConversation(11)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != 2 || first.Model != "model-b" {
		t.Fatalf("first provider = %#v", first)
	}
	if second.ID != 1 || second.Model != "model-a" {
		t.Fatalf("second provider changed = %#v", second)
	}
	defaultProvider, err := settingsSvc.DefaultProvider()
	if err != nil {
		t.Fatal(err)
	}
	if defaultProvider.ID != 1 || defaultProvider.Model != "model-a" {
		t.Fatalf("global default changed = %#v", defaultProvider)
	}
}

func TestPlanLifecycleRejectsStaleRevision(t *testing.T) {
	setupPlanTestStore(t)
	provider, err := providerForConversation(10)
	if err != nil {
		t.Fatal(err)
	}
	first, err := savePlanRevision(10, 0, 0, 101, 102, "# Plan one", provider)
	if err != nil {
		t.Fatal(err)
	}
	second, err := savePlanRevision(10, first.PlanID, first.Revision, 103, 104, "# Plan two", provider)
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != 2 {
		t.Fatalf("revision = %d, want 2", second.Revision)
	}
	if _, err := savePlanRevision(10, first.PlanID, first.Revision, 105, 106, "stale", provider); err == nil {
		t.Fatal("stale revision was accepted")
	}
	if _, err := loadPlanExecution(10, first.PlanID, first.Revision); err == nil {
		t.Fatal("stale execution was accepted")
	}
	runID, err := beginPlanRun(first.PlanID, second.Revision, "request", provider)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beginPlanRun(first.PlanID, second.Revision, "duplicate", provider); err == nil {
		t.Fatal("duplicate execution was accepted")
	}
	if err := finishPlanRun(runID, first.PlanID, planStatusFailed, "boom"); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadPlanExecution(10, first.PlanID, second.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Plan.Status != planStatusFailed || loaded.Plan.ApprovedRevision != second.Revision {
		t.Fatalf("failed plan = %#v", loaded.Plan)
	}
}

func TestPlanToolsAreReadOnly(t *testing.T) {
	tools := planAgentTools(&PermissionService{}, t.TempDir(), "session")
	names := map[string]bool{}
	for _, candidate := range tools {
		names[candidate.Declaration().Name] = true
	}
	for _, want := range []string{"list_directory", "read_file", "fetch_url", "search_workspace", "git_inspect"} {
		if !names[want] {
			t.Errorf("missing plan tool %q", want)
		}
	}
	for _, forbidden := range []string{"write_file", "execute_command", "skill_run", "publish_artifact"} {
		if names[forbidden] {
			t.Errorf("plan exposes mutating tool %q", forbidden)
		}
	}
}

func TestAttachPlanSnapshotRestoresPlanCardMetadata(t *testing.T) {
	setupPlanTestStore(t)
	if _, err := store.Exec(`
		INSERT INTO messages (id, conversation_id, role, content, message_type, created_at)
		VALUES (201, 10, 'user', 'plan it', 'plan_request', 2),
		       (202, 10, 'assistant', '# Plan', 'plan_response', 3);
		INSERT INTO message_metrics (message_id, agui_message_id, model)
		VALUES (202, 'assistant-plan', 'model-a');
		INSERT INTO plans (id, conversation_id, status, current_revision, created_at, updated_at)
		VALUES (30, 10, 'pending', 1, 2, 3);
		INSERT INTO plan_revisions (plan_id, revision, content, user_message_id, assistant_message_id, provider_id, model, created_at)
		VALUES (30, 1, '# Plan', 201, 202, 1, 'model-a', 3)`); err != nil {
		t.Fatal(err)
	}
	snapshot := map[string]any{"messages": []any{
		map[string]any{"id": "m201", "role": "user", "content": "plan it"},
		map[string]any{"id": "assistant-plan", "role": "assistant", "content": "# Plan"},
	}}
	if err := attachPlanSnapshot(snapshot, 10); err != nil {
		t.Fatal(err)
	}
	messages := snapshot["messages"].([]any)
	if messages[0].(map[string]any)["messageType"] != "plan_request" {
		t.Fatalf("request metadata = %#v", messages[0])
	}
	plan, ok := messages[1].(map[string]any)["plan"].(map[string]any)
	if !ok || plan["id"] != int64(30) || plan["revision"] != 1 || plan["status"] != planStatusPending {
		t.Fatalf("plan metadata = %#v", messages[1])
	}
}

func TestPlanRevisionSignal(t *testing.T) {
	signal := &planRevisionSignal{}
	callable, ok := newPlanRevisionTool(signal).(interface {
		Call(context.Context, []byte) (any, error)
	})
	if !ok {
		t.Skip("tool implementation does not expose direct Call")
	}
	if _, err := callable.Call(context.Background(), []byte(`{"reason":"schema changed"}`)); err != nil {
		t.Fatal(err)
	}
	requested, reason := signal.snapshot()
	if !requested || reason != "schema changed" {
		t.Fatalf("signal = %v, %q", requested, reason)
	}
}
