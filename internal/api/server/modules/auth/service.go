package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"ticketing-system/internal/middleware"
)

// ErrInvalidCredentials is returned for an unknown email and a wrong password alike,
// so a caller cannot probe which emails are registered.
var ErrInvalidCredentials = errors.New("invalid email or password")

type Token struct {
	AccessToken string
	ExpiresIn   time.Duration
	Role        string
}

type Service struct {
	users  *Repository
	secret []byte
	ttl    time.Duration
}

func NewService(users *Repository, secret []byte, ttl time.Duration) *Service {
	return &Service{users: users, secret: secret, ttl: ttl}
}

func (s *Service) Login(ctx context.Context, email, password string) (Token, error) {
	user, found, err := s.users.FindByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return Token{}, fmt.Errorf("find user: %w", err)
	}
	if !found {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		return Token{}, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return Token{}, ErrInvalidCredentials
	}

	now := time.Now()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, middleware.Claims{
		Role: user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(user.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
		},
	}).SignedString(s.secret)
	if err != nil {
		return Token{}, fmt.Errorf("sign token: %w", err)
	}

	return Token{AccessToken: signed, ExpiresIn: s.ttl, Role: user.Role}, nil
}
