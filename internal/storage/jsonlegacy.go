package storage

import (
	"encoding/json"
	"errors"
	"os"
)

// readLegacyApps reads the pre-SQLite apps.json format, read-only. It is
// the sole remaining reader of that file, kept so importLegacyApps can seed
// SQLite from it once per fresh ~/.volt directory.
func readLegacyApps(cfgDir string) ([]App, error) {
	b, err := os.ReadFile(appsPath(cfgDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out struct {
		Apps []App `json:"apps"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out.Apps, nil
}
