// Package hosts implements providers.DomainProvider (§7/§14 of the
// implementation plan) over a hosts-file-formatted text -- the same syntax
// as /etc/hosts: one mapping per line ("127.0.0.1  myapp.test"), '#'
// starting a comment.
//
// The file path is always injectable (see Provider/NewProvider) precisely
// so this can be unit-tested against a temp file -- per the plan's explicit
// instruction, tests must never touch the real /etc/hosts. New() returns a
// Provider pointed at the real, per-OS default path (path_unix.go /
// path_windows.go) for production use; nothing in this package's own tests
// calls New().
//
// Every entry Volt writes is tagged with a trailing "# volt-managed"
// comment so Add/Remove/List only ever touch Volt's own entries -- a
// foreign (hand-edited, or written by something else) line mapping the
// same hostname is left untouched and instead reported as a conflict
// (Conflicts), never silently overwritten.
package hosts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/alresiainc/alresia-voltpanel/internal/providers"
)

// managedIP is the address Volt always maps its managed hostnames to --
// every local project is reached through the loopback interface.
const managedIP = "127.0.0.1"

// managedMarker is the trailing comment Volt writes on every line it owns.
// Its presence is exactly what distinguishes a Volt-managed entry from a
// foreign one when the file is re-parsed.
const managedMarker = "volt-managed"

// ErrNotFound is returned by Remove when no Volt-managed entry exists for
// the given hostname.
var ErrNotFound = errors.New("hosts: no volt-managed entry for that hostname")

// Provider implements providers.DomainProvider by reading/writing a single
// hosts-file-formatted text file at Path.
type Provider struct {
	// Path is the hosts file this Provider reads/writes. Exported so
	// callers can see exactly which file a Provider is pointed at (useful
	// for logging/diagnostics); construct via NewProvider/New rather than
	// building the struct literal directly, so future fields don't break
	// callers.
	Path string

	mu sync.Mutex
}

// NewProvider returns a Provider bound to an explicit path -- this is the
// constructor tests use, always pointed at a t.TempDir() file, never the
// real hosts file.
func NewProvider(path string) *Provider {
	return &Provider{Path: path}
}

// New returns a Provider bound to this OS's real hosts file
// (/etc/hosts on macOS/Linux, C:\Windows\System32\drivers\etc\hosts on
// Windows -- see path_unix.go/path_windows.go). This is the production
// default; nothing in this package's automated tests calls it.
func New() *Provider {
	return &Provider{Path: defaultHostsPath()}
}

// Compile-time check that Provider satisfies the shared contract.
var _ providers.DomainProvider = (*Provider)(nil)

// line is one physical line of the hosts file. data is nil for blank lines,
// pure-comment lines, and any line this package's simple parser doesn't
// recognize as "<ip> <host...> [# comment]" -- those pass through the
// renderer completely unchanged (raw), which is what keeps Add/Remove from
// ever disturbing content Volt doesn't own.
type line struct {
	raw  string
	data *dataLine
}

type dataLine struct {
	ip      string
	hosts   []string
	comment string
	managed bool
}

func (d *dataLine) hasHost(hostname string) bool {
	for _, h := range d.hosts {
		if strings.EqualFold(h, hostname) {
			return true
		}
	}
	return false
}

func (d *dataLine) render() string {
	s := d.ip + "\t" + strings.Join(d.hosts, " ")
	if d.comment != "" {
		s += "\t# " + d.comment
	}
	return s
}

// parseAll splits hosts-file content into lines, recognizing data lines of
// the form "<ip> <host> [<host>...] [# comment]".
func parseAll(content string) []line {
	if content == "" {
		return nil
	}
	rawLines := strings.Split(content, "\n")
	out := make([]line, 0, len(rawLines))
	for _, raw := range rawLines {
		out = append(out, line{raw: raw, data: parseDataLine(raw)})
	}
	return out
}

func parseDataLine(raw string) *dataLine {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil
	}
	body := trimmed
	comment := ""
	if idx := strings.Index(trimmed, "#"); idx >= 0 {
		body = strings.TrimSpace(trimmed[:idx])
		comment = strings.TrimSpace(trimmed[idx+1:])
	}
	fields := strings.Fields(body)
	if len(fields) < 2 {
		return nil
	}
	return &dataLine{
		ip:      fields[0],
		hosts:   fields[1:],
		comment: comment,
		managed: comment == managedMarker,
	}
}

func render(lines []line) string {
	rendered := make([]string, len(lines))
	for i, l := range lines {
		if l.data != nil {
			rendered[i] = l.data.render()
		} else {
			rendered[i] = l.raw
		}
	}
	s := strings.Join(rendered, "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

func newManagedLine(hostname string) line {
	d := &dataLine{ip: managedIP, hosts: []string{hostname}, comment: managedMarker, managed: true}
	return line{data: d}
}

func (p *Provider) read() ([]line, error) {
	b, err := os.ReadFile(p.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("hosts: read %s: %w", p.Path, err)
	}
	return parseAll(string(b)), nil
}

func (p *Provider) write(lines []line) error {
	if err := os.WriteFile(p.Path, []byte(render(lines)), 0o644); err != nil {
		return fmt.Errorf("hosts: write %s: %w", p.Path, err)
	}
	return nil
}

func normalizeHostname(hostname string) (string, error) {
	h := strings.TrimSpace(hostname)
	if h == "" {
		return "", fmt.Errorf("hosts: hostname must not be empty")
	}
	if strings.ContainsAny(h, " \t#") {
		return "", fmt.Errorf("hosts: hostname %q contains invalid characters", hostname)
	}
	return h, nil
}

// Add maps hostname to 127.0.0.1 via a Volt-managed entry. It is idempotent
// -- adding an already-managed hostname succeeds without changing the file.
// If hostname is already present via a *foreign* (non-Volt) entry, Add
// refuses rather than silently taking it over; call Conflicts first to
// surface that to a user before attempting Add.
func (p *Provider) Add(_ context.Context, hostname string) error {
	host, err := normalizeHostname(hostname)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	lines, err := p.read()
	if err != nil {
		return err
	}

	for i := range lines {
		d := lines[i].data
		if d == nil || !d.hasHost(host) {
			continue
		}
		if !d.managed {
			return fmt.Errorf("hosts: %q is already present in %s and is not managed by volt", host, p.Path)
		}
		if d.ip == managedIP {
			return nil // already exactly what we want -- idempotent no-op.
		}
		d.ip = managedIP
		return p.write(lines)
	}

	lines = append(lines, newManagedLine(host))
	return p.write(lines)
}

// Remove deletes the Volt-managed entry for hostname. Returns ErrNotFound
// if no Volt-managed entry exists for it (foreign entries mapping the same
// hostname, if any, are left untouched either way -- Remove only ever
// touches what Add would have written).
func (p *Provider) Remove(_ context.Context, hostname string) error {
	host, err := normalizeHostname(hostname)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	lines, err := p.read()
	if err != nil {
		return err
	}

	kept := make([]line, 0, len(lines))
	removed := false
	for _, l := range lines {
		if l.data != nil && l.data.managed && len(l.data.hosts) == 1 && strings.EqualFold(l.data.hosts[0], host) {
			removed = true
			continue
		}
		kept = append(kept, l)
	}
	if !removed {
		return ErrNotFound
	}
	return p.write(kept)
}

// List returns every hostname currently mapped via a Volt-managed entry,
// sorted. It never reports foreign entries -- this is VoltPanel's own view
// of what it manages, not a dump of the whole hosts file.
func (p *Provider) List(_ context.Context) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	lines, err := p.read()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l.data == nil || !l.data.managed {
			continue
		}
		out = append(out, l.data.hosts...)
	}
	sort.Strings(out)
	return out, nil
}

// Conflicts reports every reason hostname cannot cleanly be Add()-ed:
// today that means a foreign (non-Volt) entry already mapping it to some
// IP. An empty, nil-error result means Add is safe to call. This only ever
// inspects the hosts file itself -- callers that also need to check other
// VoltPanel-registered domains (a different project claiming the same
// hostname) combine this with a lookup against their own domain repository
// (see internal/domain/domainname and internal/api/v1/domains.go), since
// this provider has no notion of "other projects."
func (p *Provider) Conflicts(_ context.Context, hostname string) ([]string, error) {
	host, err := normalizeHostname(hostname)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	lines, err := p.read()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range lines {
		d := l.data
		if d == nil || !d.hasHost(host) {
			continue
		}
		if !d.managed {
			out = append(out, fmt.Sprintf("%s is already mapped to %s in %s (not managed by volt)", host, d.ip, p.Path))
		} else if d.ip != managedIP {
			out = append(out, fmt.Sprintf("%s is managed by volt but points at %s instead of %s", host, d.ip, managedIP))
		}
	}
	return out, nil
}
