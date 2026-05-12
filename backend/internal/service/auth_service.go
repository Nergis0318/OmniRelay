package service

import (
	"database/sql"
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
	return &AuthService{db: db, jwtSecret: "omnirelay-jwt-secret-change-me"}
}

func (s *AuthService) SetJWTSecret(secret string) {
	s.jwtSecret = secret
}

func (s *AuthService) Register(req models.RegisterRequest) (*models.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)

	isAdmin := count == 0

	result, err := s.db.Exec(
		"INSERT INTO users (username, password_hash, is_admin) VALUES (?, ?, ?)",
		req.Username, string(hash), isAdmin,
	)
	if err != nil {
		return nil, errors.New("username already exists")
	}

	id, _ := result.LastInsertId()
	return &models.User{
		ID:        id,
		Username:  req.Username,
		IsAdmin:   isAdmin,
		CreatedAt: time.Now(),
	}, nil
}

func (s *AuthService) Login(req models.LoginRequest) (*models.LoginResponse, error) {
	var user models.User
	err := s.db.QueryRow(
		"SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = ?",
		req.Username,
	).Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt)
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
	rows, err := s.db.Query("SELECT id, username, is_admin, created_at FROM users ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}
