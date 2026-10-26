package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAuthenticationCapacity(t *testing.T) {
	auth, err := NewAuth(strings.Repeat("s", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range cap(auth.passwordSlots) {
		if err := auth.acquirePasswordSlot(ctx); err != nil {
			t.Fatal(err)
		}
	}
	s := New(nil, nil, auth)
	if _, err := s.Login(ctx, "user@example.com", "password123"); !errors.Is(err, ErrBusy) {
		t.Fatalf("login ignored authentication capacity: %v", err)
	}
	if _, err := s.Register(ctx, RegisterInput{Email: "user@example.com", Password: "password123", Name: "User"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("registration ignored authentication capacity: %v", err)
	}
	for range cap(auth.passwordSlots) {
		<-auth.passwordSlots
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := auth.acquirePasswordSlot(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request acquired capacity: %v", err)
	}
	if err := auth.acquirePasswordSlot(ctx); err != nil {
		t.Fatalf("capacity was not released: %v", err)
	}
	<-auth.passwordSlots
}

func FuzzVerifyJWT(f *testing.F) {
	auth := &Auth{secret: []byte(strings.Repeat("s", 32)), ttl: time.Hour}
	valid, err := auth.Issue(1)
	if err != nil {
		f.Fatal(err)
	}
	for _, token := range []string{"", "a.b.c", valid, strings.Repeat(".", 5000)} {
		f.Add(token)
	}
	f.Fuzz(func(t *testing.T, token string) {
		if len(token) > 16<<10 {
			t.Skip()
		}
		id, err := auth.Verify(token)
		if err == nil && id <= 0 {
			t.Fatal("accepted an invalid subject")
		}
	})
}

func TestJWT(t *testing.T) {
	auth, err := NewAuth(strings.Repeat("a", 32), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.Issue(42)
	if err != nil {
		t.Fatal(err)
	}
	if id, err := auth.Verify(token); err != nil || id != 42 {
		t.Fatalf("Verify = %d, %v", id, err)
	}
	now := time.Now()
	tests := []struct {
		name   string
		method jwt.SigningMethod
		claims jwt.RegisteredClaims
		key    any
	}{
		{"expired", jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "secunda", Audience: []string{"secunda-api"}, Subject: "42", ExpiresAt: jwt.NewNumericDate(now.Add(-time.Hour))}, auth.secret},
		{"missing expiration", jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "secunda", Audience: []string{"secunda-api"}, Subject: "42"}, auth.secret},
		{"wrong issuer", jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "other", Audience: []string{"secunda-api"}, Subject: "42", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}, auth.secret},
		{"wrong audience", jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "secunda", Audience: []string{"other"}, Subject: "42", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}, auth.secret},
		{"wrong algorithm", jwt.SigningMethodHS384, jwt.RegisteredClaims{Issuer: "secunda", Audience: []string{"secunda-api"}, Subject: "42", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}, auth.secret},
		{"wrong key", jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "secunda", Audience: []string{"secunda-api"}, Subject: "42", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}, []byte(strings.Repeat("b", 32))},
		{"invalid subject", jwt.SigningMethodHS256, jwt.RegisteredClaims{Issuer: "secunda", Audience: []string{"secunda-api"}, Subject: "0", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}, auth.secret},
		{"unsigned", jwt.SigningMethodNone, jwt.RegisteredClaims{Issuer: "secunda", Audience: []string{"secunda-api"}, Subject: "42", ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour))}, jwt.UnsafeAllowNoneSignatureType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := jwt.NewWithClaims(tt.method, tt.claims).SignedString(tt.key)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := auth.Verify(raw); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("Verify error = %v", err)
			}
		})
	}
}
