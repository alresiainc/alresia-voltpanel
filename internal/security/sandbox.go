package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sandbox restricts filesystem operations to a single root directory,
// rejecting paths that traverse outside of it or escape it via a symlink.
type Sandbox struct {
	root string
}

// NewSandbox creates a Sandbox rooted at root. The root is resolved to its
// real (symlink-free) path so later comparisons are exact.
func NewSandbox(root string) (*Sandbox, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		resolved = abs
	}
	return &Sandbox{root: resolved}, nil
}

// Root returns the sandbox's real root path.
func (s *Sandbox) Root() string { return s.root }

// Resolve turns a user-supplied path (absolute, or relative to the sandbox
// root) into a real filesystem path guaranteed to live inside the sandbox
// root. It rejects ".." traversal and symlink escapes.
func (s *Sandbox) Resolve(userPath string) (string, error) {
	if userPath == "" {
		userPath = "."
	}
	var joined string
	if filepath.IsAbs(userPath) {
		joined = filepath.Clean(userPath)
	} else {
		joined = filepath.Join(s.root, userPath)
	}

	// Resolve symlinks on the deepest existing ancestor so a symlink
	// anywhere in the path (or a symlinked ancestor of the sandbox root
	// itself, e.g. macOS's /var -> /private/var) can't produce a false
	// traversal result. This is the sole containment check: it's stricter
	// than comparing the un-resolved `joined` path against root, since it
	// also catches ".." segments that only escape after Clean().
	real, err := resolveExistingAncestor(joined)
	if err != nil {
		return "", err
	}
	if !isWithin(s.root, real) {
		return "", fmt.Errorf("path escapes sandbox root via symlink: %s", userPath)
	}
	return joined, nil
}

func isWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolveExistingAncestor walks up from path until it finds a segment that
// exists, resolves symlinks on that segment, and rejoins the remainder.
func resolveExistingAncestor(path string) (string, error) {
	cur := path
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(append([]string{resolved}, suffix...)...), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			// reached filesystem root without finding anything real
			return path, nil
		}
		suffix = append([]string{filepath.Base(cur)}, suffix...)
		cur = parent
	}
}
