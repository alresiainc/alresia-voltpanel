package server

import (
	"context"
	"errors"
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/alresiainc/alresia-voltpanel/internal/storage"
)

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	dir := t.TempDir()
	st, err := storage.NewStoreWithRoot(dir, dir)
	if err != nil {
		t.Fatalf("NewStoreWithRoot: %v", err)
	}
	return NewRepository(st.DB())
}

func TestRepository_CreateGetListDelete(t *testing.T) {
	repo := newTestRepo(t)

	s, err := repo.Create(CreateRequest{Name: "box1", Hostname: "example.internal", Username: "dev"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if s.ID == "" {
		t.Fatal("Create returned empty ID")
	}
	if s.Port != 22 {
		t.Fatalf("default port = %d, want 22", s.Port)
	}
	if s.AuthMethod != "agent" {
		t.Fatalf("default auth method = %q, want %q", s.AuthMethod, "agent")
	}

	got, err := repo.Get(s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Hostname != "example.internal" {
		t.Fatalf("Get hostname = %q, want %q", got.Hostname, "example.internal")
	}

	list, err := repo.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List returned %d servers, want 1", len(list))
	}

	if err := repo.Delete(s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: err = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(s.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete of already-deleted server: err = %v, want ErrNotFound", err)
	}
}

func TestRepository_Create_Validation(t *testing.T) {
	repo := newTestRepo(t)

	cases := []CreateRequest{
		{Hostname: "h", Username: "u"},                                    // missing name
		{Name: "n", Username: "u"},                                        // missing hostname
		{Name: "n", Hostname: "h"},                                        // missing username
		{Name: "n", Hostname: "h", Username: "u", AuthMethod: "password"}, // unsupported auth method
		{Name: "n", Hostname: "h", Username: "u", AuthMethod: "key"},      // key auth with no secret_ref
	}
	for i, req := range cases {
		if _, err := repo.Create(req); err == nil {
			t.Errorf("case %d: Create(%+v) unexpectedly succeeded", i, req)
		}
	}
}

func TestRepository_Get_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	if _, err := repo.Get("does-not-exist"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get: err = %v, want ErrNotFound", err)
	}
}

// fakeProvider/fakeSession are minimal in-package stand-ins for
// providers.RemoteProvider/RemoteSession -- enough to unit-test
// TestConnection's own logic (recording last_connected_at/os_info on
// success, propagating errors on failure) without a real network. The
// real, non-mocked SSH exchange is covered by
// internal/providers/remote/ssh's tests and by the API-level end-to-end
// test against the in-process fake sshd.
type fakeProvider struct {
	connectErr error
	session    *fakeSession
}

func (f *fakeProvider) Connect(ctx context.Context, s providers.Server) (providers.RemoteSession, error) {
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	return f.session, nil
}

type fakeSession struct {
	execErr   error
	closed    bool
	execCalls []string
}

func (f *fakeSession) Exec(ctx context.Context, command string) (stdout, stderr []byte, err error) {
	f.execCalls = append(f.execCalls, command)
	if f.execErr != nil {
		return nil, []byte("boom"), f.execErr
	}
	if command == "uname -a" {
		return []byte("Linux fakehost 6.0.0\n"), nil, nil
	}
	return nil, nil, nil
}
func (f *fakeSession) ListDir(ctx context.Context, path string) ([]providers.RemoteFileInfo, error) {
	return nil, nil
}
func (f *fakeSession) ReadFile(ctx context.Context, path string) ([]byte, error)     { return nil, nil }
func (f *fakeSession) WriteFile(ctx context.Context, path string, data []byte) error { return nil }
func (f *fakeSession) Metrics(ctx context.Context) (providers.RemoteMetrics, error) {
	return providers.RemoteMetrics{}, nil
}
func (f *fakeSession) Close() error { f.closed = true; return nil }

func TestRepository_TestConnection_Success(t *testing.T) {
	repo := newTestRepo(t)
	s, err := repo.Create(CreateRequest{Name: "box1", Hostname: "h", Username: "u"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess := &fakeSession{}
	prov := &fakeProvider{session: sess}

	if err := repo.TestConnection(context.Background(), prov, s.ID); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	if !sess.closed {
		t.Error("TestConnection did not close the session")
	}
	if len(sess.execCalls) == 0 || sess.execCalls[0] != "true" {
		t.Errorf("TestConnection exec calls = %v, want first call to be %q", sess.execCalls, "true")
	}

	got, err := repo.Get(s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastConnectedAt == nil {
		t.Error("LastConnectedAt not recorded after a successful TestConnection")
	}
	if got.OSInfo != "Linux fakehost 6.0.0" {
		t.Errorf("OSInfo = %q, want %q", got.OSInfo, "Linux fakehost 6.0.0")
	}
}

func TestRepository_TestConnection_ConnectFailure(t *testing.T) {
	repo := newTestRepo(t)
	s, err := repo.Create(CreateRequest{Name: "box1", Hostname: "h", Username: "u"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	prov := &fakeProvider{connectErr: errors.New("dial refused")}
	if err := repo.TestConnection(context.Background(), prov, s.ID); err == nil {
		t.Fatal("TestConnection unexpectedly succeeded")
	}

	got, err := repo.Get(s.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastConnectedAt != nil {
		t.Error("LastConnectedAt recorded despite a failed TestConnection")
	}
}

func TestRepository_TestConnection_ExecFailure(t *testing.T) {
	repo := newTestRepo(t)
	s, err := repo.Create(CreateRequest{Name: "box1", Hostname: "h", Username: "u"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sess := &fakeSession{execErr: errors.New("command failed")}
	prov := &fakeProvider{session: sess}
	if err := repo.TestConnection(context.Background(), prov, s.ID); err == nil {
		t.Fatal("TestConnection unexpectedly succeeded")
	}
	if !sess.closed {
		t.Error("TestConnection did not close the session on exec failure")
	}
}

func TestRepository_TestConnection_UnknownServer(t *testing.T) {
	repo := newTestRepo(t)
	prov := &fakeProvider{session: &fakeSession{}}
	if err := repo.TestConnection(context.Background(), prov, "no-such-id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("TestConnection: err = %v, want ErrNotFound", err)
	}
}
