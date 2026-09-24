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
	"ticketing-system/internal/pkg/util"
	"ticketing-system/pkg/logger"
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
	log    *logger.Logger
}

func NewService(users *Repository, secret []byte, ttl time.Duration, log *logger.Logger) *Service {
	return &Service{users: users, secret: secret, ttl: ttl, log: log}
}

func (s *Service) Login(ctx context.Context, email, password string) (Token, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, found, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return Token{}, fmt.Errorf("find user: %w", err)
	}
	if !found {
		_ = bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte(password))
		s.loginFailed(ctx, email, 0, reasonUnknownEmail)
		return Token{}, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		s.loginFailed(ctx, email, user.ID, reasonWrongPassword)
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

// loginFailed logs why a login failed, which the caller is never told: repeated wrong_password for one
// user_id is password guessing on that account, many unknown_email is someone probing for accounts.
// The email is masked and the password is never logged.
func (s *Service) loginFailed(ctx context.Context, email string, userID int64, reason string) {
	fields := map[string]any{
		"request_id": middleware.RequestIDFrom(ctx),
		"email":      util.MaskEmail(email),
		"reason":     reason,
	}
	if userID != 0 {
		fields["user_id"] = userID
	}
	s.log.Warn("login failed", fields)
}
