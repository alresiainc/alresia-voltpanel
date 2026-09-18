// Package ssh implements providers.RemoteProvider (§7/§17 Phase 7) over
// golang.org/x/crypto/ssh. It never assumes a Volt daemon runs on the far
// end: every operation is a plain command or an SFTP call any stock sshd
// already supports.
//
// Auth prefers the user's existing ssh-agent (talking to $SSH_AUTH_SOCK)
// over importing/storing a key at all, per §9.9. A key-based fallback is
// supported when a KeyResolver is wired -- it resolves key material through
// the Secret abstraction (internal/security.SecretStore), never a plaintext
// key file under the config dir.
package ssh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// KeyResolver resolves the plaintext private-key material (PEM bytes) for a
// server configured with auth_method "key", given its secret_ref. Wired to
// internal/security.SecretStore.Resolve in production; servers using
// auth_method "agent" never call this.
type KeyResolver func(secretRef string) ([]byte, error)

const defaultDialTimeout = 10 * time.Second

// Provider implements providers.RemoteProvider.
type Provider struct {
	// DialTimeout bounds the TCP+SSH handshake. Defaults to 10s when zero.
	DialTimeout time.Duration
	// ResolveKey resolves key material for auth_method=="key" servers.
	// Left nil, key-based auth is simply unavailable (agent-only).
	ResolveKey KeyResolver
	// AgentSocket overrides $SSH_AUTH_SOCK -- mainly for tests.
	AgentSocket string
	// HostKeyCallback overrides the default. Left nil, Connect uses
	// ssh.InsecureIgnoreHostKey(): Volt manages developer-owned dev/VPS
	// boxes added one at a time through its own UI, not a fleet with a
	// pre-shared known_hosts, so there's no trust-anchor to verify against
	// yet. This is a known, deliberate gap (not a silent one) -- a real
	// known_hosts/TOFU-pinning callback is future work, not attempted here
	// so it isn't half-built and quietly wrong.
	HostKeyCallback ssh.HostKeyCallback
}

// New returns a Provider using ssh-agent-only auth and no host key
// verification. Set ResolveKey to also support auth_method=="key" servers.
func New() *Provider { return &Provider{} }

func (p *Provider) dialTimeout() time.Duration {
	if p.DialTimeout > 0 {
		return p.DialTimeout
	}
	return defaultDialTimeout
}

func (p *Provider) hostKeyCallback() ssh.HostKeyCallback {
	if p.HostKeyCallback != nil {
		return p.HostKeyCallback
	}
	return ssh.InsecureIgnoreHostKey()
}

// Connect establishes one SSH connection to server and returns a Session
// wrapping it.
func (p *Provider) Connect(ctx context.Context, server providers.Server) (providers.RemoteSession, error) {
	auths, err := p.authMethods(server)
	if err != nil {
		return nil, err
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("no usable SSH auth method for %s@%s (auth_method=%q): is ssh-agent running with a loaded key?", server.Username, server.Hostname, server.AuthMethod)
	}

	cfg := &ssh.ClientConfig{
		User:            server.Username,
		Auth:            auths,
		HostKeyCallback: p.hostKeyCallback(),
		Timeout:         p.dialTimeout(),
	}

	addr := net.JoinHostPort(server.Hostname, strconv.Itoa(portOrDefault(server.Port)))
	dialer := net.Dialer{Timeout: p.dialTimeout()}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh handshake with %s: %w", addr, err)
	}
	return &Session{client: ssh.NewClient(clientConn, chans, reqs)}, nil
}

func portOrDefault(port int) int {
	if port <= 0 {
		return 22
	}
	return port
}

// authMethods builds the []ssh.AuthMethod list for server per its
// auth_method: "agent" (default) prefers the running ssh-agent; "key" goes
// through ResolveKey (the Secret abstraction). No plaintext key file is
// ever read from disk here.
func (p *Provider) authMethods(server providers.Server) ([]ssh.AuthMethod, error) {
	switch server.AuthMethod {
	case "", "agent":
		am, ok := p.agentAuth()
		if !ok {
			return nil, nil
		}
		return []ssh.AuthMethod{am}, nil
	case "key":
		if p.ResolveKey == nil {
			return nil, fmt.Errorf("server %s is configured for key auth but no key resolver is wired", server.Hostname)
		}
		keyPEM, err := p.ResolveKey(server.SecretRef)
		if err != nil {
			return nil, fmt.Errorf("resolve ssh key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(keyPEM)
		if err != nil {
			return nil, fmt.Errorf("parse ssh key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	default:
		return nil, fmt.Errorf("unsupported auth_method %q", server.AuthMethod)
	}
}

// agentAuth dials $SSH_AUTH_SOCK (or AgentSocket, for tests) and returns an
// ssh.AuthMethod backed by whatever keys the running ssh-agent holds. ok is
// false when no agent socket is configured/reachable -- not itself an
// error, since a server might still be reachable via key auth instead.
func (p *Provider) agentAuth() (ssh.AuthMethod, bool) {
	sock := p.AgentSocket
	if sock == "" {
		sock = os.Getenv("SSH_AUTH_SOCK")
	}
	if sock == "" {
		return nil, false
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, false
	}
	ag := agent.NewClient(conn)
	return ssh.PublicKeysCallback(ag.Signers), true
}

// Session implements providers.RemoteSession over one ssh.Client.
type Session struct {
	client *ssh.Client
}

// Exec runs command in a fresh SSH session and returns its captured
// stdout/stderr. If ctx is cancelled before the command exits, the
// underlying session is closed and ctx.Err() is returned alongside
// whatever output had been captured so far.
func (s *Session) Exec(ctx context.Context, command string) (stdout, stderr []byte, err error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return nil, nil, fmt.Errorf("open ssh session: %w", err)
	}
	defer sess.Close()

	var outBuf, errBuf bytes.Buffer
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf

	done := make(chan error, 1)
	go func() { done <- sess.Run(command) }()

	select {
	case <-ctx.Done():
		_ = sess.Close()
		return outBuf.Bytes(), errBuf.Bytes(), ctx.Err()
	case runErr := <-done:
		return outBuf.Bytes(), errBuf.Bytes(), runErr
	}
}

func (s *Session) sftpClient() (*sftp.Client, error) {
	cl, err := sftp.NewClient(s.client)
	if err != nil {
		return nil, fmt.Errorf("open sftp client: %w", err)
	}
	return cl, nil
}

// ListDir lists one remote directory over SFTP.
func (s *Session) ListDir(ctx context.Context, dir string) ([]providers.RemoteFileInfo, error) {
	cl, err := s.sftpClient()
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	entries, err := cl.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("sftp readdir %s: %w", dir, err)
	}
	out := make([]providers.RemoteFileInfo, 0, len(entries))
	for _, e := range entries {
		out = append(out, providers.RemoteFileInfo{
			Name:    e.Name(),
			Path:    joinRemotePath(dir, e.Name()),
			IsDir:   e.IsDir(),
			Size:    e.Size(),
			Mode:    e.Mode().String(),
			ModTime: e.ModTime(),
		})
	}
	return out, nil
}

// ReadFile reads one remote file's full contents over SFTP.
func (s *Session) ReadFile(ctx context.Context, path string) ([]byte, error) {
	cl, err := s.sftpClient()
	if err != nil {
		return nil, err
	}
	defer cl.Close()

	f, err := cl.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sftp open %s: %w", path, err)
	}
	defer f.Close()
	return io.ReadAll(f)
}

// WriteFile writes (creating or truncating) one remote file over SFTP.
func (s *Session) WriteFile(ctx context.Context, path string, data []byte) error {
	cl, err := s.sftpClient()
	if err != nil {
		return err
	}
	defer cl.Close()

	f, err := cl.Create(path)
	if err != nil {
		return fmt.Errorf("sftp create %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("sftp write %s: %w", path, err)
	}
	return nil
}

// Metrics reports a best-effort snapshot of the remote host by running
// `uptime` and reading /proc/meminfo -- both are near-universal on Linux
// remotes; failures are tolerated individually so one missing command
// doesn't blank out the whole response. This deliberately stays simple
// (§17 Phase 7: "don't over-engineer this part") -- it's what lets a remote
// server's status look roughly like a local service's to the UI, not a
// full monitoring integration.
func (s *Session) Metrics(ctx context.Context) (providers.RemoteMetrics, error) {
	m := providers.RemoteMetrics{Raw: map[string]string{}}

	out, _, err := s.Exec(ctx, "uptime")
	if err != nil {
		return m, fmt.Errorf("uptime: %w", err)
	}
	m.Uptime = strings.TrimSpace(string(out))
	m.LoadAverage = parseLoadAverage(m.Uptime)

	if memOut, _, memErr := s.Exec(ctx, "cat /proc/meminfo"); memErr == nil {
		parseMemInfo(string(memOut), &m)
	}

	return m, nil
}

func (s *Session) Close() error { return s.client.Close() }

// joinRemotePath joins a remote (always POSIX-style, per SFTP) directory
// and entry name -- deliberately not path/filepath, which is OS-dependent
// and would use backslashes when Volt itself runs on Windows connecting to
// a Linux remote.
func joinRemotePath(dir, name string) string {
	if dir == "" || dir == "/" {
		return "/" + name
	}
	return strings.TrimRight(dir, "/") + "/" + name
}

// parseLoadAverage best-effort extracts the "load average: ..." suffix from
// a standard `uptime` line. Returns "" if the format doesn't match (e.g. a
// BSD/macOS remote with a different uptime format) rather than erroring.
func parseLoadAverage(uptimeLine string) string {
	const marker = "load average:"
	idx := strings.Index(uptimeLine, marker)
	if idx == -1 {
		return ""
	}
	return strings.TrimSpace(uptimeLine[idx+len(marker):])
}

// parseMemInfo best-effort extracts MemTotal/MemFree(or MemAvailable) from
// /proc/meminfo's "Key:    12345 kB" lines, and stashes every parsed field
// under Raw for anything not promoted to its own struct field.
func parseMemInfo(text string, m *providers.RemoteMetrics) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		m.Raw[key] = fields[1]
		v, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			m.MemTotalKB = v
		case "MemAvailable":
			m.MemFreeKB = v
		case "MemFree":
			if m.MemFreeKB == 0 {
				m.MemFreeKB = v
			}
		}
	}
}
