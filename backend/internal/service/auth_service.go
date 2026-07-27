package service

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"omnirelay/internal/models"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	db        *sql.DB
	jwtSecret string
}

func NewAuthService(db *sql.DB) *AuthService {
	return &AuthService{db: db}
}

func (s *AuthService) SetJWTSecret(secret string) {
	s.jwtSecret = secret
}

func (s *AuthService) Register(req models.RegisterRequest) (*models.User, error) {
	exists, err := s.emailExists(req.Email)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errors.New("email already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)

	isAdmin := count == 0

	result, err := s.db.Exec(
		"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)",
		req.Username, req.Email, string(hash), isAdmin,
	)
	if err != nil {
		if exists, existsErr := s.emailExists(req.Email); existsErr == nil && exists {
			return nil, errors.New("email already exists")
		}
		return nil, err
	}

	id, _ := result.LastInsertId()
	return &models.User{
		ID:        id,
		Username:  req.Username,
		Email:     req.Email,
		IsAdmin:   isAdmin,
		CreatedAt: time.Now(),
	}, nil
}

func (s *AuthService) emailExists(email string) (bool, error) {
	var existing int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", email).Scan(&existing); err != nil {
		return false, err
	}
	return existing > 0, nil
}

func (s *AuthService) Login(req models.LoginRequest) (*models.LoginResponse, error) {
	var user models.User
	err := s.db.QueryRow(
		"SELECT id, username, email, password_hash, is_admin, created_at FROM users WHERE email = ?",
		req.Email,
	).Scan(&user.ID, &user.Username, &user.Email, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, errors.New("invalid credentials")
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  user.ID,
		"username": user.Username,
		"is_admin": user.IsAdmin,
		"exp":      time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(s.jwtSecret))
	if err != nil {
		return nil, err
	}

	return &models.LoginResponse{
		Token: tokenString,
		User:  user,
	}, nil
}

func (s *AuthService) ListUsers() ([]models.User, error) {
	rows, err := s.db.Query("SELECT id, username, email, is_admin, created_at FROM users ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *AuthService) DeleteUser(id int64, requesterID int64) error {
	if id == requesterID {
		return errors.New("cannot delete yourself")
	}
	if err := s.ensureNotLastAdmin(id); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func (s *AuthService) SetRole(id int64, isAdmin bool) error {
	if !isAdmin {
		if err := s.ensureNotLastAdmin(id); err != nil {
			return err
		}
	}
	_, err := s.db.Exec("UPDATE users SET is_admin = ? WHERE id = ?", isAdmin, id)
	return err
}

func (s *AuthService) ensureNotLastAdmin(userID int64) error {
	var isAdmin bool
	err := s.db.QueryRow("SELECT is_admin FROM users WHERE id = ?", userID).Scan(&isAdmin)
	if err != nil {
		return errors.New("user not found")
	}
	if !isAdmin {
		return nil
	}
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&count)
	if count <= 1 {
		return errors.New("cannot remove the last admin")
	}
	return nil
}

func (s *AuthService) GenerateResetCode(userID int64) (string, error) {
	var exists int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM users WHERE id = ?", userID).Scan(&exists); err != nil || exists == 0 {
		return "", errors.New("user not found")
	}

	code, err := generateResetToken()
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(15 * time.Minute)
	_, err = s.db.Exec(
		"INSERT INTO password_reset_codes (user_id, code, expires_at) VALUES (?, ?, ?)",
		userID, code, expiresAt,
	)
	if err != nil {
		return "", err
	}
	return code, nil
}

func (s *AuthService) ResetPasswordWithCode(code, newPassword string) error {
	var userID int64
	var expiresAt time.Time
	var used bool
	err := s.db.QueryRow(
		"SELECT user_id, expires_at, used FROM password_reset_codes WHERE code = ?", code,
	).Scan(&userID, &expiresAt, &used)
	if err != nil {
		return errors.New("invalid reset code")
	}
	if used {
		return errors.New("code already used")
	}
	if time.Now().After(expiresAt) {
		return errors.New("code expired")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("UPDATE users SET password_hash = ? WHERE id = ?", string(hash), userID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE password_reset_codes SET used = 1 WHERE code = ?", code); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *AuthService) GetUserProviders(userID int64) ([]int64, error) {
	rows, err := s.db.Query("SELECT provider_id FROM user_providers WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// AllowedProviderSet returns the set of provider IDs the user may access.
// A nil map means all providers are allowed (no ACL rows configured).
func (s *AuthService) AllowedProviderSet(userID int64) (map[int64]bool, error) {
	ids, err := s.GetUserProviders(userID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}

func (s *AuthService) SetUserProviders(userID int64, providerIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM user_providers WHERE user_id = ?", userID); err != nil {
		return err
	}
	for _, pid := range providerIDs {
		if _, err := tx.Exec("INSERT INTO user_providers (user_id, provider_id) VALUES (?, ?)", userID, pid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *AuthService) CanAccessProvider(userID, providerID int64) (bool, error) {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM user_providers WHERE user_id = ?", userID).Scan(&count); err != nil {
		return false, err
	}
	if count == 0 {
		return true, nil
	}
	var allowed int
	err := s.db.QueryRow("SELECT COUNT(*) FROM user_providers WHERE user_id = ? AND provider_id = ?", userID, providerID).Scan(&allowed)
	if err != nil {
		return false, err
	}
	return allowed > 0, nil
}

func generateResetToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
