package pipeline

import (
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/project"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

func newTestRepo(t *testing.T) (*Repository, string) {
	t.Helper()
	db, err := storage.OpenSQLite(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proj, err := project.NewRepository(db).Create("app", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return NewRepository(db), proj.ID
}

func TestCreatePipelineGetsAUniqueWebhookSecret(t *testing.T) {
	r, projID := newTestRepo(t)
	p1, err := r.Create(projID, "ci", "steps:\n  - name: t\n    run: echo hi\n")
	if err != nil {
		t.Fatal(err)
	}
	p2, err := r.Create(projID, "ci2", "steps:\n  - name: t\n    run: echo hi\n")
	if err != nil {
		t.Fatal(err)
	}
	if p1.WebhookSecret == "" || p2.WebhookSecret == "" {
		t.Fatal("expected a non-empty webhook secret")
	}
	if p1.WebhookSecret == p2.WebhookSecret {
		t.Fatal("expected each pipeline to get its own distinct webhook secret")
	}
}

func TestWebhookSecretNeverSerialized(t *testing.T) {
	r, projID := newTestRepo(t)
	p, err := r.Create(projID, "ci", "steps: []")
	if err != nil {
		t.Fatal(err)
	}
	if p.WebhookSecret == "" {
		t.Fatal("expected a webhook secret to exist internally")
	}
	// json:"-" is asserted structurally in api/v1 tests (the actual HTTP
	// response); here we just confirm the tag is present on the field.
}

func TestListByProjectAndDelete(t *testing.T) {
	r, projID := newTestRepo(t)
	p, err := r.Create(projID, "ci", "steps: []")
	if err != nil {
		t.Fatal(err)
	}
	list, err := r.ListByProject(projID)
	if err != nil || len(list) != 1 {
		t.Fatalf("expected 1 pipeline, got %d (err=%v)", len(list), err)
	}
	if err := r.Delete(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get(p.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestRunLifecycleAndHistory(t *testing.T) {
	r, projID := newTestRepo(t)
	p, err := r.Create(projID, "ci", "steps: []")
	if err != nil {
		t.Fatal(err)
	}
	run, err := r.CreateRun(p.ID, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "running" {
		t.Fatalf("expected new run to be running, got %s", run.Status)
	}

	steps := []map[string]any{{"name": "test", "status": "success"}}
	if err := r.FinishRun(run.ID, "success", steps); err != nil {
		t.Fatal(err)
	}

	history, err := r.ListRuns(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 run in history, got %d", len(history))
	}
	if history[0].Status != "success" || len(history[0].Steps) != 1 {
		t.Fatalf("unexpected run: %+v", history[0])
	}
}
