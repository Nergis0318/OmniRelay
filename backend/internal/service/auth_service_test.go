package service

import (
	"path/filepath"
	"testing"

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
