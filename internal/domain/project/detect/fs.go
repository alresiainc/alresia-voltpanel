package detect

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// fileExists reports whether dir/name exists and is a regular file (not a
// directory).
func fileExists(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}

// composerJSON is the small slice of composer.json this package cares
// about -- just enough to spot a Laravel dependency.
type composerJSON struct {
	Require map[string]string `json:"require"`
}

// readComposerJSON reads dir/composer.json. ok is false (with a nil error)
// when the file simply doesn't exist.
func readComposerJSON(dir string) (c *composerJSON, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(dir, "composer.json"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var parsed composerJSON
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, false, err
	}
	return &parsed, true, nil
}

// packageJSON is the small slice of package.json this package cares about.
type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Scripts         map[string]string `json:"scripts"`
}

// readPackageJSON reads dir/package.json. ok is false (with a nil error)
// when the file simply doesn't exist.
func readPackageJSON(dir string) (p *packageJSON, ok bool, err error) {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var parsed packageJSON
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, false, err
	}
	return &parsed, true, nil
}
