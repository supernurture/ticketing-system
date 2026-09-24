package auth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"ticketing-system/internal/middleware"
	"ticketing-system/pkg/logger"
)

const secret = "a-test-secret-that-is-at-least-32-chars"

// password matches dummyHash, so dummyHash doubles as a stored hash.
const password = "Customer123!"

func mockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
	service, mock, _ := mockServiceWithLog(t)
	return service, mock
}

// mockServiceWithLog also returns the log directory.
func mockServiceWithLog(t *testing.T) (*Service, sqlmock.Sqlmock, string) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}
	logDir := t.TempDir()
	log, err := logger.New(logger.Config{ServiceName: "test", Path: logDir})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })

	return NewService(NewRepository(db), []byte(secret), time.Hour, log), mock, logDir
}

// loginFailedLine returns the "login failed" log line.
func loginFailedLine(t *testing.T, logDir string) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(logDir, "test", "*.log"))
	for _, name := range files {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, line := range strings.Split(string(contents), "\n") {
			if strings.Contains(line, `"login failed"`) {
				return line
			}
		}
	}
	t.Fatal("no login failed line in the log")
	return ""
}

func expectUser(mock sqlmock.Sqlmock, email string, found bool) {
	rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "role"})
	if found {
		rows.AddRow(int64(7), email, dummyHash, "customer")
	}
	mock.ExpectQuery(`SELECT id, email, password_hash, role FROM users WHERE email = \$1`).
		WithArgs(email).WillReturnRows(rows)
}

func TestLoginIssuesATokenForTheRightPassword(t *testing.T) {
	service, mock := mockService(t)
	expectUser(mock, "customer@mkp.test", true)

	// Email is trimmed and lower-cased before lookup.
	token, err := service.Login(context.Background(), "  Customer@MKP.test ", password)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	var claims middleware.Claims
	if _, err := jwt.ParseWithClaims(token.AccessToken, &claims, func(*jwt.Token) (any, error) {
		return []byte(secret), nil
	}); err != nil {
		t.Fatalf("issued token does not verify: %v", err)
	}
	if claims.Subject != "7" || claims.Role != "customer" || token.Role != "customer" {
		t.Errorf("claims = %+v, want subject 7 and role customer", claims)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

// Same error for both failures; only the log tells them apart.
func TestLoginRejectsWrongPasswordAndUnknownEmailAlike(t *testing.T) {
	tests := map[string]struct {
		email, password string
		found           bool
		wantLog         []string
		notInLog        []string
	}{
		"wrong password": {
			email: "customer@mkp.test", password: "guess-123", found: true,
			wantLog:  []string{`"reason":"wrong_password"`, `"user_id":7`, `"email":"c***@mkp.test"`},
			notInLog: []string{"customer@mkp.test", "guess-123"},
		},
		"unknown email": {
			email: "nobody@mkp.test", password: password, found: false,
			wantLog:  []string{`"reason":"unknown_email"`, `"email":"n***@mkp.test"`},
			notInLog: []string{"nobody@mkp.test", password, `"user_id"`},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			service, mock, logDir := mockServiceWithLog(t)
			expectUser(mock, tc.email, tc.found)

			_, err := service.Login(context.Background(), tc.email, tc.password)
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Errorf("error = %v, want ErrInvalidCredentials", err)
			}

			line := loginFailedLine(t, logDir)
			for _, want := range tc.wantLog {
				if !strings.Contains(line, want) {
					t.Errorf("log line is missing %s: %s", want, line)
				}
			}
			for _, leak := range tc.notInLog {
				if strings.Contains(line, leak) {
					t.Errorf("log line contains %s: %s", leak, line)
				}
			}
		})
	}
}

func TestLoginSurfacesDatabaseFailure(t *testing.T) {
	service, mock := mockService(t)
	mock.ExpectQuery(`FROM users WHERE email = \$1`).WillReturnError(errors.New("connection reset"))

	_, err := service.Login(context.Background(), "customer@mkp.test", password)
	if err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("error = %v, want the database failure rather than a credentials error", err)
	}
}
