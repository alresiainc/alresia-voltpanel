package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// This file's tests exercise a real, in-process SSH protocol exchange --
// no mocking at the ssh.Client/ssh.ServerConn boundary -- per the plan's
// explicit guidance that SSH auth edge cases are exactly where mocks lie.
// golang.org/x/crypto/ssh supports acting as a server directly inside a Go
// test (ssh.NewServerConn over a plain net.Listener with a throwaway host
// key), so no Docker/system sshd is needed. No real external host is ever
// contacted by anything in this file.

// fakeSSHServer is a minimal, real sshd-alike used only by these tests: it
// accepts one authorized public key, runs a small set of exec commands for
// real over the SSH channel, and serves a real SFTP subsystem rooted at a
// temp directory via github.com/pkg/sftp's server implementation.
type fakeSSHServer struct {
	addr string
}

func startFakeSSHServer(t *testing.T, authorizedKey ssh.PublicKey) *fakeSSHServer {
	t.Helper()

	hostKey := generateSigner(t)

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if authorizedKey != nil && string(key.Marshal()) == string(authorizedKey.Marshal()) {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("unauthorized public key")
		},
	}
	config.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			nConn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeConn(nConn, config)
		}
	}()

	return &fakeSSHServer{addr: ln.Addr().String()}
}

func handleFakeConn(nConn net.Conn, config *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go handleFakeSession(channel, requests)
	}
}

func handleFakeSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()
	for req := range requests {
		switch req.Type {
		case "exec":
			var payload struct{ Command string }
			_ = ssh.Unmarshal(req.Payload, &payload)
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			runFakeCommand(channel, payload.Command)
			return
		case "subsystem":
			var payload struct{ Subsystem string }
			_ = ssh.Unmarshal(req.Payload, &payload)
			if payload.Subsystem != "sftp" {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				continue
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			srv, err := sftp.NewServer(channel)
			if err == nil {
				_ = srv.Serve()
			}
			return
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

// runFakeCommand answers a fixed set of commands the way a real Linux
// remote plausibly would, over a genuine SSH exec channel (request/reply,
// stdout/stderr streams, exit-status request) -- only the *content* of
// "uptime"/"/proc/meminfo" is canned, not the transport.
func runFakeCommand(channel ssh.Channel, command string) {
	exitCode := uint32(0)
	switch {
	case command == "true":
		// no output, exit 0
	case command == "false":
		exitCode = 1
	case strings.HasPrefix(command, "echo "):
		fmt.Fprintf(channel, "%s\n", strings.TrimPrefix(command, "echo "))
	case command == "uptime":
		fmt.Fprint(channel, " 12:34:56 up 10 days,  3:21,  2 users,  load average: 0.10, 0.20, 0.30\n")
	case command == "cat /proc/meminfo":
		fmt.Fprint(channel, "MemTotal:       16384000 kB\nMemFree:         2048000 kB\nMemAvailable:    8192000 kB\n")
	default:
		fmt.Fprintf(channel.Stderr(), "unknown command: %s\n", command)
		exitCode = 127
	}
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{exitCode}))
}

func generateSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer from key: %v", err)
	}
	return signer
}

// generatePEMKeyPair returns a PKCS8-PEM-encoded ed25519 private key
// (as SecretStore/KeyResolver would hand back) plus its ssh.Signer/
// PublicKey, for the auth_method=="key" test path.
func generatePEMKeyPair(t *testing.T) (pemBytes []byte, signer ssh.Signer) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err = ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer from key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemBytes = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	return pemBytes, signer
}

// startFakeSSHAgent serves the real agent wire protocol
// (golang.org/x/crypto/ssh/agent) over a unix socket backed by signer, so
// Provider's agent-auth path exercises a genuine SSH agent round trip
// rather than a fake ssh.AuthMethod stitched in directly.
func startFakeSSHAgent(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	// A short-lived dir directly under the OS temp root, not t.TempDir()
	// (which nests under a long per-test-name path) -- AF_UNIX socket
	// paths are limited to ~104 bytes on macOS/BSD, and a long test name
	// blows through that ("bind: invalid argument").
	dir, err := os.MkdirTemp("", "volt-ssh-agent")
	if err != nil {
		t.Fatalf("mkdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockPath := filepath.Join(dir, "a.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv}); err != nil {
		t.Fatalf("add key to agent keyring: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _ = agent.ServeAgent(keyring, conn) }()
		}
	}()

	return sockPath
}

func TestSSHProvider_AgentAuth_ConnectExecSFTPMetrics(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}

	srv := startFakeSSHServer(t, signer.PublicKey())
	agentSock := startFakeSSHAgent(t, priv)

	host, portStr, err := net.SplitHostPort(srv.addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	p := &Provider{AgentSocket: agentSock, DialTimeout: 5 * time.Second}
	server := providers.Server{ID: "s1", Hostname: host, Port: port, Username: "tester", AuthMethod: "agent"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := p.Connect(ctx, server)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer sess.Close()

	// TestConnection-shaped check: exec "true" and expect success.
	stdout, stderr, err := sess.Exec(ctx, "true")
	if err != nil {
		t.Fatalf(`Exec("true"): %v (stderr=%s)`, err, stderr)
	}
	if len(stdout) != 0 {
		t.Fatalf(`Exec("true") stdout = %q, want empty`, stdout)
	}

	// A command whose stdout we can assert on exactly.
	stdout, _, err = sess.Exec(ctx, "echo hello-from-fake-remote")
	if err != nil {
		t.Fatalf("Exec(echo): %v", err)
	}
	if got := strings.TrimSpace(string(stdout)); got != "hello-from-fake-remote" {
		t.Fatalf("Exec(echo) stdout = %q, want %q", got, "hello-from-fake-remote")
	}

	// A failing command should report a non-nil error (non-zero exit).
	if _, _, err := sess.Exec(ctx, "false"); err == nil {
		t.Fatal(`Exec("false") returned nil error, want a non-zero-exit error`)
	}

	// SFTP file browsing: write a file directly on "disk" (i.e. wherever
	// the fake server's sftp.NewServer(channel) actually operates -- the
	// real local filesystem, since pkg/sftp's default handlers just wrap
	// os.* calls) and read/list it back over the wire.
	dir := t.TempDir()
	filePath := filepath.Join(dir, "hello.txt")
	const content = "hello over sftp\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	entries, err := sess.ListDir(ctx, dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Name == "hello.txt" {
			found = true
			if e.IsDir {
				t.Fatal("hello.txt reported as a directory")
			}
		}
	}
	if !found {
		t.Fatalf("ListDir(%s) did not include hello.txt: %+v", dir, entries)
	}

	got, err := sess.ReadFile(ctx, filePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != content {
		t.Fatalf("ReadFile = %q, want %q", got, content)
	}

	const written = "written back over sftp\n"
	if err := sess.WriteFile(ctx, filePath, []byte(written)); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	onDisk, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read back from disk: %v", err)
	}
	if string(onDisk) != written {
		t.Fatalf("file on disk = %q, want %q", onDisk, written)
	}

	// Metrics: best-effort uptime/meminfo parse from our fake command
	// responses.
	m, err := sess.Metrics(ctx)
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.LoadAverage != "0.10, 0.20, 0.30" {
		t.Errorf("LoadAverage = %q, want %q", m.LoadAverage, "0.10, 0.20, 0.30")
	}
	if m.MemTotalKB != 16384000 {
		t.Errorf("MemTotalKB = %d, want 16384000", m.MemTotalKB)
	}
	if m.MemFreeKB != 8192000 {
		t.Errorf("MemFreeKB (from MemAvailable) = %d, want 8192000", m.MemFreeKB)
	}
}

func TestSSHProvider_KeyAuth_Connect(t *testing.T) {
	pemBytes, signer := generatePEMKeyPair(t)
	srv := startFakeSSHServer(t, signer.PublicKey())

	host, portStr, err := net.SplitHostPort(srv.addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	resolveCalls := 0
	p := &Provider{
		DialTimeout: 5 * time.Second,
		ResolveKey: func(secretRef string) ([]byte, error) {
			resolveCalls++
			if secretRef != "secret-ref-123" {
				return nil, fmt.Errorf("unexpected secretRef %q", secretRef)
			}
			return pemBytes, nil
		},
	}
	server := providers.Server{ID: "s2", Hostname: host, Port: port, Username: "tester", AuthMethod: "key", SecretRef: "secret-ref-123"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	sess, err := p.Connect(ctx, server)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer sess.Close()

	if resolveCalls != 1 {
		t.Fatalf("ResolveKey called %d times, want 1", resolveCalls)
	}

	if _, _, err := sess.Exec(ctx, "true"); err != nil {
		t.Fatalf(`Exec("true"): %v`, err)
	}
}

func TestSSHProvider_NoAuthMethodAvailable(t *testing.T) {
	p := &Provider{AgentSocket: filepath.Join(t.TempDir(), "no-such-agent.sock")}
	server := providers.Server{Hostname: "127.0.0.1", Port: 22, Username: "tester", AuthMethod: "agent"}

	_, err := p.Connect(context.Background(), server)
	if err == nil {
		t.Fatal("Connect with no reachable agent and no key resolver unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "no usable SSH auth method") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSSHProvider_UnauthorizedKeyRejected(t *testing.T) {
	// Server only trusts one key; the client (via a *different* agent-held
	// key) must be rejected -- a real handshake failure, not a mocked one.
	trustedSigner := generateSigner(t)
	srv := startFakeSSHServer(t, trustedSigner.PublicKey())

	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	agentSock := startFakeSSHAgent(t, otherPriv)

	host, portStr, err := net.SplitHostPort(srv.addr)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	p := &Provider{AgentSocket: agentSock, DialTimeout: 5 * time.Second}
	server := providers.Server{Hostname: host, Port: port, Username: "tester", AuthMethod: "agent"}

	if _, err := p.Connect(context.Background(), server); err == nil {
		t.Fatal("Connect with an untrusted key unexpectedly succeeded")
	}
}
