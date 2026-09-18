package pluginhost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
	"github.com/google/uuid"
)

// ManifestFileName is the well-known manifest filename LoadExtension looks
// for inside an extension's directory (§17 Phase 11's "manifest format").
const ManifestFileName = "volt-extension.json"

// Manifest describes an extension before the daemon ever runs it -- name,
// version, the provider kind it implements, how to launch it, and the
// permissions it's asking for. LoadExtension reads this first, so the
// daemon knows what it's agreeing to run before running it (per the task:
// "LoadExtension reads this before even starting the subprocess").
type Manifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	// Kind is the provider category this extension implements: "runtime"
	// is the only one internal/domain/extension knows how to register
	// today (matching providers.RuntimeProvider); other kinds are
	// accepted here (so a manifest never fails to parse) but rejected at
	// Enable time until a later phase adds support.
	Kind string `json:"kind"`
	// Executable is the command to run -- either a bare name looked up on
	// PATH (e.g. "python3") or a path resolved relative to the manifest's
	// own directory. Args are passed as-is; the subprocess's working
	// directory is always the manifest's directory, so a relative script
	// path in Args (e.g. "provider.py") resolves naturally.
	Executable  string   `json:"executable"`
	Args        []string `json:"args,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// Validate checks that every field LoadExtension needs is present.
func (m Manifest) Validate() error {
	var missing []string
	if m.Name == "" {
		missing = append(missing, "name")
	}
	if m.Version == "" {
		missing = append(missing, "version")
	}
	if m.Kind == "" {
		missing = append(missing, "kind")
	}
	if m.Executable == "" {
		missing = append(missing, "executable")
	}
	if len(missing) > 0 {
		return fmt.Errorf("pluginhost: manifest missing required field(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

// ReadManifest reads and validates the manifest at path, which may either
// be the manifest file itself or a directory containing ManifestFileName.
// It returns the parsed Manifest and the manifest's own directory (the
// extension's root -- used to resolve Executable/Args and as the
// subprocess's working directory).
func ReadManifest(path string) (Manifest, string, error) {
	manifestPath := path
	info, err := os.Stat(path)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("pluginhost: reading manifest at %q: %w", path, err)
	}
	if info.IsDir() {
		manifestPath = filepath.Join(path, ManifestFileName)
	}
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("pluginhost: reading manifest %q: %w", manifestPath, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, "", fmt.Errorf("pluginhost: parsing manifest %q: %w", manifestPath, err)
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, "", err
	}
	return m, filepath.Dir(manifestPath), nil
}

// DefaultHandshakeTimeout/DefaultRequestTimeout bound how long LoadExtension
// and ExternalProvider's calls wait for the subprocess before deciding it
// has misbehaved and giving up with a clear error rather than hanging.
// Package vars (not consts) so tests can shrink them -- same pattern as
// internal/domain/service's HealthCheckDelay.
var (
	DefaultHandshakeTimeout = 5 * time.Second
	DefaultRequestTimeout   = 10 * time.Second
)

// ExternalProvider is a running extension subprocess. It implements
// providers.RuntimeProvider (and, if a later phase widens the protocol,
// other provider interfaces) by translating each Go method call into one
// Request/Response line over the subprocess's stdio -- from the
// providers.Registry's point of view it is indistinguishable from a
// built-in provider.
type ExternalProvider struct {
	manifest  Manifest
	handshake Handshake
	dir       string

	cmd            *exec.Cmd
	stdin          io.WriteCloser
	scanner        *bufio.Scanner
	requestTimeout time.Duration

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

var _ providers.RuntimeProvider = (*ExternalProvider)(nil)

// LoadExtension reads the manifest at path (a directory containing
// ManifestFileName, or the manifest file itself), launches its executable
// as a subprocess, and performs the handshake. It refuses to load an
// extension whose declared protocolVersion this build doesn't understand
// (§21's versioning concern), and cleans up the subprocess on any failure
// rather than leaving an orphaned or half-initialized process behind.
func LoadExtension(path string) (*ExternalProvider, error) {
	manifest, dir, err := ReadManifest(path)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(manifest.Executable, manifest.Args...)
	cmd.Dir = dir

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("pluginhost: extension %q: stdin pipe: %w", manifest.Name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pluginhost: extension %q: stdout pipe: %w", manifest.Name, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("pluginhost: extension %q: stderr pipe: %w", manifest.Name, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pluginhost: extension %q: start %q: %w", manifest.Name, manifest.Executable, err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	ep := &ExternalProvider{
		manifest:       manifest,
		dir:            dir,
		cmd:            cmd,
		stdin:          stdin,
		scanner:        scanner,
		requestTimeout: DefaultRequestTimeout,
		done:           make(chan struct{}),
	}

	go func() {
		_ = cmd.Wait()
		close(ep.done)
	}()
	go drainStderr(manifest.Name, stderr)

	handshake, err := ep.readHandshake(DefaultHandshakeTimeout)
	if err != nil {
		_ = ep.Close()
		return nil, err
	}
	if !IsSupportedVersion(handshake.ProtocolVersion) {
		_ = ep.Close()
		return nil, fmt.Errorf(
			"pluginhost: extension %q declared protocol version %d, which this VoltPanel build does not understand (supported: %v) -- refusing to load it",
			manifest.Name, handshake.ProtocolVersion, SupportedVersions,
		)
	}

	ep.mu.Lock()
	ep.handshake = handshake
	ep.mu.Unlock()
	return ep, nil
}

func drainStderr(name string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		log.Printf("volt: extension %q stderr: %s", name, scanner.Text())
	}
}

// readLineLocked reads exactly one line from the subprocess's stdout,
// waiting at most timeout (or until ctx is done, when ctx is non-nil).
// Must be called with mu held. A clean EOF (subprocess closed stdout,
// e.g. because it exited) is reported as an error, since this protocol
// never expects the extension to close the stream on its own initiative
// mid-conversation.
func (e *ExternalProvider) readLineLocked(ctx context.Context, timeout time.Duration) ([]byte, error) {
	type result struct {
		line []byte
		eof  bool
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		if e.scanner.Scan() {
			ch <- result{line: append([]byte(nil), e.scanner.Bytes()...)}
			return
		}
		if err := e.scanner.Err(); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{eof: true}
	}()

	var timer *time.Timer
	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}
	var ctxDone <-chan struct{}
	if ctx != nil {
		ctxDone = ctx.Done()
	}

	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("pluginhost: extension %q: reading from subprocess: %w", e.manifest.Name, r.err)
		}
		if r.eof {
			return nil, fmt.Errorf("pluginhost: extension %q: subprocess closed its output unexpectedly", e.manifest.Name)
		}
		return r.line, nil
	case <-timeoutCh:
		return nil, fmt.Errorf("pluginhost: extension %q: timed out after %s waiting for a response", e.manifest.Name, timeout)
	case <-ctxDone:
		return nil, fmt.Errorf("pluginhost: extension %q: %w", e.manifest.Name, ctx.Err())
	}
}

func (e *ExternalProvider) readHandshake(timeout time.Duration) (Handshake, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	line, err := e.readLineLocked(nil, timeout)
	if err != nil {
		return Handshake{}, fmt.Errorf("pluginhost: extension handshake failed: %w", err)
	}
	return DecodeHandshake(line)
}

// call sends one Request for method/params and waits for the matching
// Response, enforcing e.requestTimeout (and ctx's own deadline/
// cancellation) so a subprocess that hangs on a request can never hang
// the caller -- it gets killed and a clear error is returned instead.
func (e *ExternalProvider) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, fmt.Errorf("pluginhost: extension %q is closed", e.manifest.Name)
	}

	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("pluginhost: extension %q: marshal params for %s: %w", e.manifest.Name, method, err)
		}
		raw = b
	}

	id := uuid.NewString()
	req := Request{ID: id, Method: method, Params: raw}
	reqLine, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("pluginhost: extension %q: marshal request: %w", e.manifest.Name, err)
	}
	reqLine = append(reqLine, '\n')

	if _, err := e.stdin.Write(reqLine); err != nil {
		e.closeLocked()
		return nil, fmt.Errorf("pluginhost: extension %q: writing request %s: %w", e.manifest.Name, method, err)
	}

	line, err := e.readLineLocked(ctx, e.requestTimeout)
	if err != nil {
		e.closeLocked()
		return nil, err
	}

	resp, err := DecodeResponse(line)
	if err != nil {
		e.closeLocked()
		return nil, fmt.Errorf("pluginhost: extension %q: malformed response to %s: %w", e.manifest.Name, method, err)
	}
	if resp.ID != id {
		e.closeLocked()
		return nil, fmt.Errorf("pluginhost: extension %q: protocol violation: response id %q does not match request id %q", e.manifest.Name, resp.ID, id)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("pluginhost: extension %q: %s: %s", e.manifest.Name, method, resp.Error)
	}
	return resp.Result, nil
}

// closeLocked kills the subprocess (if still alive) and waits briefly for
// it to be reaped. Must be called with mu held. Safe to call more than
// once.
func (e *ExternalProvider) closeLocked() {
	if e.closed {
		return
	}
	e.closed = true
	_ = e.stdin.Close()
	if e.cmd.Process != nil {
		_ = e.cmd.Process.Kill()
	}
	select {
	case <-e.done:
	case <-time.After(2 * time.Second):
	}
}

// Close stops the extension's subprocess. Safe to call multiple times and
// safe to call even if the process already exited on its own.
func (e *ExternalProvider) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closeLocked()
	return nil
}

// Kind returns the provider name the extension declared in its handshake
// (e.g. "python") -- this is what providers.Registry keys it under,
// exactly like a built-in RuntimeProvider's Kind().
func (e *ExternalProvider) Kind() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.handshake.Name
}

// runtimeVersionWire is the wire shape for one entry of DetectInstalled's
// result -- kept separate from providers.RuntimeVersion (which has no
// json tags of its own) so the protocol's JSON shape stays stable and
// documented regardless of that Go struct's internal field names.
type runtimeVersionWire struct {
	Version     string `json:"version"`
	InstallPath string `json:"installPath"`
	IsDefault   bool   `json:"isDefault"`
}

// DetectInstalled implements providers.RuntimeProvider by calling the
// extension's "DetectInstalled" method and decoding its result.
func (e *ExternalProvider) DetectInstalled(ctx context.Context) ([]providers.RuntimeVersion, error) {
	result, err := e.call(ctx, "DetectInstalled", nil)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}
	var wire []runtimeVersionWire
	if err := json.Unmarshal(result, &wire); err != nil {
		return nil, fmt.Errorf("pluginhost: extension %q: malformed DetectInstalled result: %w", e.manifest.Name, err)
	}
	out := make([]providers.RuntimeVersion, 0, len(wire))
	for _, w := range wire {
		out = append(out, providers.RuntimeVersion{Version: w.Version, InstallPath: w.InstallPath, IsDefault: w.IsDefault})
	}
	return out, nil
}

// Install implements providers.RuntimeProvider by calling the extension's
// "Install" method. progress is accepted for interface compatibility but
// not (yet) streamed over the protocol -- every call is request/response,
// not a subscription; a later protocol version could add progress
// notifications without breaking this one.
func (e *ExternalProvider) Install(ctx context.Context, version string, progress providers.ProgressFunc) error {
	_, err := e.call(ctx, "Install", map[string]string{"version": version})
	return err
}

// Remove implements providers.RuntimeProvider by calling the extension's
// "Remove" method.
func (e *ExternalProvider) Remove(ctx context.Context, version string) error {
	_, err := e.call(ctx, "Remove", map[string]string{"version": version})
	return err
}

// SetDefault implements providers.RuntimeProvider by calling the
// extension's "SetDefault" method.
func (e *ExternalProvider) SetDefault(ctx context.Context, version string) error {
	_, err := e.call(ctx, "SetDefault", map[string]string{"version": version})
	return err
}
