package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

type Auth struct {
	secret        []byte
	ttl           time.Duration
	dummyHash     []byte
	passwordSlots chan struct{}
}

func NewAuth(secret string, ttl time.Duration) (*Auth, error) {
	if len(secret) < 32 || ttl <= 0 {
		return nil, fmt.Errorf("JWT secret must contain at least 32 bytes and TTL must be positive")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("invalid-login-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	return &Auth{
		secret:        []byte(secret),
		ttl:           ttl,
		dummyHash:     hash,
		passwordSlots: make(chan struct{}, 4),
	}, nil
}

func (a *Auth) Issue(userID int64) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    "secunda",
		Audience:  jwt.ClaimStrings{"secunda-api"},
		Subject:   strconv.FormatInt(userID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(a.ttl)),
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

func (a *Auth) Verify(raw string) (int64, error) {
	if len(raw) > 4096 {
		return 0, ErrUnauthorized
	}

	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return a.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer("secunda"),
		jwt.WithAudience("secunda-api"), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil {
		return 0, ErrUnauthorized
	}

	id, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0, ErrUnauthorized
	}

	return id, nil
}

func (a *Auth) acquirePasswordSlot(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	select {
	case a.passwordSlots <- struct{}{}:
		return nil
	default:
		return ErrBusy
	}
}

type RegisterInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (User, error) {
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email || len(input.Email) > 254 ||
		!validText(input.Name, 1, 100) || len(input.Password) < 8 || len(input.Password) > 72 {
		return User{}, fmt.Errorf("%w: valid email, name (1–100 characters) and password (8–72 bytes) required", ErrInvalid)
	}

	if err := s.auth.acquirePasswordSlot(ctx); err != nil {
		return User{}, err
	}
	defer func() { <-s.auth.passwordSlots }()
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	user := User{
		Email:     input.Email,
		Name:      input.Name,
		CreatedAt: nowUTC(),
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO users (email, password_hash, name, created_at) VALUES (?, ?, ?, ?)`,
		user.Email, string(hash), user.Name, user.CreatedAt)
	if err != nil {
		return User{}, dbError(err)
	}
	user.ID, err = result.LastInsertId()
	return user, err
}

func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) == 0 || len(email) > 254 || len(password) < 8 || len(password) > 72 {
		return "", ErrUnauthorized
	}

	if err := s.auth.acquirePasswordSlot(ctx); err != nil {
		return "", err
	}
	defer func() { <-s.auth.passwordSlots }()

	var id int64
	var hash []byte
	err := s.db.QueryRowContext(ctx, `SELECT id, password_hash FROM users WHERE email = ?`,
		email).Scan(&id, &hash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if errors.Is(err, sql.ErrNoRows) {
		hash = s.auth.dummyHash
	}

	if bcrypt.CompareHashAndPassword(hash, []byte(password)) != nil || id == 0 {
		return "", ErrUnauthorized
	}

	return s.auth.Issue(id)
}
