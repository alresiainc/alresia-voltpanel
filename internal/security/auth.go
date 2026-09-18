package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"time"
)

// SessionCookieName is the cookie both the HTTP API and the WebSocket
// upgrade handshake look for -- shared here so internal/ws doesn't need to
// import internal/api/v1 (which would be a cycle) just for the name.
const SessionCookieName = "volt_session"

// SessionAuth issues and verifies short-lived, stateless session tokens
// signed with a server-local secret. It sits on top of the daemon's static
// bearer token (§9.2 of the plan): a client presents the static token once
// to /auth/token/verify and gets a session token back, carried afterward as
// an HttpOnly cookie so the browser doesn't need to hold the raw token in
// JS-reachable storage.
type SessionAuth struct {
	secret []byte
}

const (
	sessionNonceLen = 16
	sessionMACLen   = 32 // full HMAC-SHA256
)

func NewSessionAuth(secretB64 string) (*SessionAuth, error) {
	b, err := base64.StdEncoding.DecodeString(secretB64)
	if err != nil {
		return nil, fmt.Errorf("decode session secret: %w", err)
	}
	if len(b) < 32 {
		return nil, fmt.Errorf("session secret too short")
	}
	return &SessionAuth{secret: b}, nil
}

// NewSessionSecret generates a fresh base64-encoded secret suitable for
// storing in Config.SessionSecret.
func NewSessionSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// Issue returns a new session token valid for ttl.
func (s *SessionAuth) Issue(ttl time.Duration) string {
	payload := make([]byte, 8+sessionNonceLen)
	binary.BigEndian.PutUint64(payload[:8], uint64(time.Now().Add(ttl).Unix()))
	_, _ = rand.Read(payload[8:])
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(append(payload, sig...))
}

// Verify reports whether token is a session token this SessionAuth issued
// and that hasn't expired yet.
func (s *SessionAuth) Verify(token string) bool {
	if token == "" {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 8+sessionNonceLen+sessionMACLen {
		return false
	}
	payload, sig := raw[:8+sessionNonceLen], raw[8+sessionNonceLen:]
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(payload)
	expected := mac.Sum(nil)
	if subtle.ConstantTimeCompare(sig, expected) != 1 {
		return false
	}
	expiry := int64(binary.BigEndian.Uint64(payload[:8]))
	return time.Now().Unix() < expiry
}
