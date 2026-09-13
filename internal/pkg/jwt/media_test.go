package jwt

import (
	"testing"
	"time"
)

const testSecret = "test-media-secret"

func TestMediaTokenRoundTripCarriesScope(t *testing.T) {
	token, err := GenerateMediaToken(12, 31, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateMediaToken: %v", err)
	}

	claims, err := ParseMediaToken(token, testSecret)
	if err != nil {
		t.Fatalf("ParseMediaToken: %v", err)
	}
	if claims.UserID != 12 || claims.TaskID != 31 {
		t.Fatalf("claims scope = user %d task %d, want user 12 task 31", claims.UserID, claims.TaskID)
	}
	if claims.Purpose != MediaStreamPurpose {
		t.Fatalf("purpose = %q, want %q", claims.Purpose, MediaStreamPurpose)
	}
}

func TestMediaTokenRejectsWrongSecret(t *testing.T) {
	token, err := GenerateMediaToken(12, 31, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateMediaToken: %v", err)
	}
	if _, err := ParseMediaToken(token, "other-secret"); err == nil {
		t.Fatal("expected wrong secret to be rejected")
	}
}

func TestMediaTokenRejectsExpired(t *testing.T) {
	token, err := GenerateMediaToken(12, 31, testSecret, -time.Minute)
	if err != nil {
		t.Fatalf("GenerateMediaToken: %v", err)
	}
	if _, err := ParseMediaToken(token, testSecret); err == nil {
		t.Fatal("expected expired token to be rejected")
	}
}

// A session token must not be usable as a playback credential: it is long-lived,
// carries no task scope, and is the credential most likely to leak from history.
func TestMediaTokenRejectsSessionToken(t *testing.T) {
	session, err := GenerateToken(12, "test", "DEMO", testSecret, 72)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if _, err := ParseMediaToken(session, testSecret); err == nil {
		t.Fatal("expected session token to be rejected as a media credential")
	}
}

// A media token must not be usable as a session token, so a shared stream URL
// cannot be replayed against authenticated API routes.
func TestSessionParserRejectsMediaToken(t *testing.T) {
	media, err := GenerateMediaToken(12, 31, testSecret, time.Hour)
	if err != nil {
		t.Fatalf("GenerateMediaToken: %v", err)
	}
	if _, err := ParseToken(media, testSecret); err == nil {
		t.Fatal("expected media token to be rejected by the session parser")
	}
}
