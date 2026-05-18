package service

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"omnirelay/internal/database"
	"omnirelay/internal/models"
)

func newTestAuthService(t *testing.T) *AuthService {
	t.Helper()
	db, err := database.Init(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewAuthService(db)
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	svc := newTestAuthService(t)

	_, err := svc.Register(models.RegisterRequest{
		Username: "alice",
		Email:    "same@example.com",
		Password: "password1",
	})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}

	_, err = svc.Register(models.RegisterRequest{
		Username: "bob",
		Email:    "same@example.com",
		Password: "password2",
	})
	if err == nil || err.Error() != "email already exists" {
		t.Fatalf("expected duplicate email error, got %v", err)
	}
}

func TestRegisterFirstUserBecomesAdmin(t *testing.T) {
	svc := newTestAuthService(t)

	first, err := svc.Register(models.RegisterRequest{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password1",
	})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	if !first.IsAdmin {
		t.Errorf("first user should be admin, got IsAdmin=false")
	}

	second, err := svc.Register(models.RegisterRequest{
		Username: "bob",
		Email:    "bob@example.com",
		Password: "password2",
	})
	if err != nil {
		t.Fatalf("second register: %v", err)
	}
	if second.IsAdmin {
		t.Errorf("second user should not be admin, got IsAdmin=true")
	}
}

func TestLoginIssuesJWTOnSuccess(t *testing.T) {
	svc := newTestAuthService(t)
	const secret = "test-secret-for-jwt-signing"
	svc.SetJWTSecret(secret)

	user, err := svc.Register(models.RegisterRequest{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password1",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	resp, err := svc.Login(models.LoginRequest{
		Email:    "alice@example.com",
		Password: "password1",
	})
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected JWT token, got empty string")
	}
	if resp.User.ID != user.ID {
		t.Errorf("user.ID = %d, want %d", resp.User.ID, user.ID)
	}

	// Verify the JWT can be parsed with the configured secret and contains the expected claims.
	parsed, err := jwt.Parse(resp.Token, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("returned token did not validate against configured secret: err=%v valid=%v", err, parsed.Valid)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["username"] != "alice" {
		t.Errorf("claims.username = %v, want alice", claims["username"])
	}
	if claims["is_admin"] != true {
		t.Errorf("first-user claims.is_admin = %v, want true", claims["is_admin"])
	}
}

func TestLoginRejectsBadPassword(t *testing.T) {
	svc := newTestAuthService(t)
	_, err := svc.Register(models.RegisterRequest{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "correct-password",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err = svc.Login(models.LoginRequest{
		Email:    "alice@example.com",
		Password: "wrong-password",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid credentials") {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
}

func TestLoginRejectsUnknownEmail(t *testing.T) {
	svc := newTestAuthService(t)
	_, err := svc.Login(models.LoginRequest{
		Email:    "nobody@example.com",
		Password: "x",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid credentials") {
		t.Fatalf("expected invalid credentials for unknown email, got %v", err)
	}
}

func TestListUsersReturnsRegisteredUsersInOrder(t *testing.T) {
	svc := newTestAuthService(t)
	for _, email := range []string{"alice@example.com", "bob@example.com", "carol@example.com"} {
		if _, err := svc.Register(models.RegisterRequest{
			Username: strings.Split(email, "@")[0],
			Email:    email,
			Password: "password1",
		}); err != nil {
			t.Fatalf("register %s: %v", email, err)
		}
	}
	users, err := svc.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("len(users) = %d, want 3", len(users))
	}
	wantEmails := []string{"alice@example.com", "bob@example.com", "carol@example.com"}
	for i, u := range users {
		if u.Email != wantEmails[i] {
			t.Errorf("users[%d].Email = %q, want %q", i, u.Email, wantEmails[i])
		}
	}
	if !users[0].IsAdmin || users[1].IsAdmin || users[2].IsAdmin {
		t.Errorf("only the first user should be admin, got %v / %v / %v", users[0].IsAdmin, users[1].IsAdmin, users[2].IsAdmin)
	}
}
