package v1

import (
	"database/sql"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
)

// audit writes an AuditEvent row for every mutating call (§9.8) -- this is
// what makes "who did what and when" answerable later without retrofitting
// logging into every provider as it's added. actor is always "local" today
// (§9's single-trusted-operator model); it becomes meaningful once multiple
// credentials/sessions can exist.
//
// An optional trailing command string (used today only by execServer,
// where the executed command is itself the security-relevant detail per
// §9.8) is stored in metadata_json rather than added as its own column --
// keeps this signature stable for every other call site.
func audit(db *sql.DB, action, targetType, targetID, result string, command ...string) {
	metadata := "{}"
	if len(command) > 0 {
		if b, err := json.Marshal(map[string]string{"command": command[0]}); err == nil {
			metadata = string(b)
		}
	}
	_, err := db.Exec(
		`INSERT INTO audit_events (id, actor, action, target_type, target_id, result, created_at, metadata_json) VALUES (?, 'local', ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), action, targetType, targetID, result, time.Now().UTC().Format(time.RFC3339), metadata,
	)
	if err != nil {
		log.Printf("volt: audit log write failed: %v", err)
	}
}
