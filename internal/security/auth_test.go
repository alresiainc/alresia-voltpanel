package security

import (
	"testing"
	"time"
)

func TestSessionAuthIssueAndVerify(t *testing.T) {
	secret, err := NewSessionSecret()
	if err != nil {
		t.Fatal(err)
	}
	sa, err := NewSessionAuth(secret)
	if err != nil {
		t.Fatal(err)
	}

	tok := sa.Issue(time.Hour)
	if !sa.Verify(tok) {
		t.Fatal("expected freshly issued token to verify")
	}
}

func TestSessionAuthRejectsForgedToken(t *testing.T) {
	secret1, _ := NewSessionSecret()
	secret2, _ := NewSessionSecret()
	sa1, _ := NewSessionAuth(secret1)
	sa2, _ := NewSessionAuth(secret2)

	tok := sa1.Issue(time.Hour)
	if sa2.Verify(tok) {
		t.Fatal("token signed with a different secret must not verify")
	}
}

func TestSessionAuthRejectsExpired(t *testing.T) {
	secret, _ := NewSessionSecret()
	sa, _ := NewSessionAuth(secret)

	tok := sa.Issue(-time.Second) // already expired
	if sa.Verify(tok) {
		t.Fatal("expected expired token to be rejected")
	}
}

func TestSessionAuthRejectsGarbage(t *testing.T) {
	secret, _ := NewSessionSecret()
	sa, _ := NewSessionAuth(secret)

	for _, bad := range []string{"", "not-base64!!", "aGVsbG8"} {
		if sa.Verify(bad) {
			t.Fatalf("expected garbage token %q to be rejected", bad)
		}
	}
}
