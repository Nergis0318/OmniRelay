package handlers

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseWSUserIDRejectsNonHS256(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS512, jwt.MapClaims{
		"user_id": float64(42),
	})
	tokenStr, err := token.SignedString([]byte("ws-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := parseWSUserID(tokenStr, "ws-secret"); err == nil {
		t.Fatal("expected HS512 token to be rejected, got nil error")
	}
}

func TestParseWSUserIDAcceptsHS256(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(42),
	})
	tokenStr, err := token.SignedString([]byte("ws-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	userID, err := parseWSUserID(tokenStr, "ws-secret")
	if err != nil {
		t.Fatalf("parseWSUserID: %v", err)
	}
	if userID != 42 {
		t.Fatalf("userID = %d, want 42", userID)
	}
}

func TestParseWSUserIDRejectsMissingUserID(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"username": "alice",
	})
	tokenStr, err := token.SignedString([]byte("ws-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := parseWSUserID(tokenStr, "ws-secret"); err == nil {
		t.Fatal("expected token without user_id to be rejected")
	}
}

func TestParseWSUserIDRejectsWrongSecret(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": float64(42),
	})
	tokenStr, err := token.SignedString([]byte("other-secret"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	if _, err := parseWSUserID(tokenStr, "ws-secret"); err == nil {
		t.Fatal("expected token signed with wrong secret to be rejected")
	}
}

func TestIsWSOriginAllowed(t *testing.T) {
	allowed := []string{"http://localhost:5173", "https://relay.example.com"}

	cases := []struct {
		origin string
		host   string
		want   bool
	}{
		{"http://localhost:5173", "relay.example.com", true},
		{"https://relay.example.com", "other.example.com", true},
		{"https://relay.example.com", "relay.example.com", true},
		{"https://evil.example.com", "relay.example.com", false},
		{"http://localhost:5174", "relay.example.com", false},
		{"null", "relay.example.com", false},
		// Same-origin dashboard connections are always allowed.
		{"https://relay.example.com", "relay.example.com", true},
		{"http://relay.example.com", "relay.example.com", true},
		// No Origin header: rejected only when an allow list is configured.
		{"", "relay.example.com", false},
	}
	for _, tc := range cases {
		if got := isWSOriginAllowed(tc.origin, tc.host, allowed); got != tc.want {
			t.Errorf("isWSOriginAllowed(%q, %q) = %v, want %v", tc.origin, tc.host, got, tc.want)
		}
	}
}

func TestIsWSOriginAllowedEmptyListAllowsAny(t *testing.T) {
	if !isWSOriginAllowed("https://anything.example.com", "relay.example.com", nil) {
		t.Fatal("empty allow list should permit any origin (legacy behavior)")
	}
	if !isWSOriginAllowed("", "relay.example.com", nil) {
		t.Fatal("empty allow list should permit requests without Origin (legacy behavior)")
	}
}
