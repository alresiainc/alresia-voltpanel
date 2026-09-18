package v1

import (
	"database/sql"
	"log"
	"time"

	"github.com/google/uuid"
)

// audit writes an AuditEvent row for every mutating call (§9.8) -- this is
// what makes "who did what and when" answerable later without retrofitting
// logging into every provider as it's added. actor is always "local" today
// (§9's single-trusted-operator model); it becomes meaningful once multiple
// credentials/sessions can exist.
func audit(db *sql.DB, action, targetType, targetID, result string) {
	_, err := db.Exec(
		`INSERT INTO audit_events (id, actor, action, target_type, target_id, result, created_at) VALUES (?, 'local', ?, ?, ?, ?, ?)`,
		uuid.NewString(), action, targetType, targetID, result, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		log.Printf("volt: audit log write failed: %v", err)
	}
}
