package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"ticketing-system/internal/middleware"
)

const secret = "a-test-secret-that-is-at-least-32-chars"

// dummyHash is the bcrypt hash of this password, so it doubles as a stored user's hash.
const password = "Customer123!"

func mockService(t *testing.T) (*Service, sqlmock.Sqlmock) {
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
	return NewService(NewRepository(db), []byte(secret), time.Hour), mock
}

func expectUser(mock sqlmock.Sqlmock, email string, found bool) {
	rows := sqlmock.NewRows([]string{"id", "email", "password_hash", "role"})
	if found {
		rows.AddRow(int64(7), email, string(dummyHash), "customer")
	}
	mock.ExpectQuery(`SELECT \* FROM "users" WHERE email = \$1`).WithArgs(email, 1).WillReturnRows(rows)
}

func TestLoginIssuesATokenForTheRightPassword(t *testing.T) {
	service, mock := mockService(t)
	expectUser(mock, "customer@mkp.test", true)

	// Mixed case and spaces are normalised to the stored lower-case email.
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

func TestLoginRejectsWrongPasswordAndUnknownEmailAlike(t *testing.T) {
	t.Run("wrong password", func(t *testing.T) {
		service, mock := mockService(t)
		expectUser(mock, "customer@mkp.test", true)

		_, err := service.Login(context.Background(), "customer@mkp.test", "wrong")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("error = %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("unknown email", func(t *testing.T) {
		service, mock := mockService(t)
		expectUser(mock, "nobody@mkp.test", false)

		_, err := service.Login(context.Background(), "nobody@mkp.test", password)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("error = %v, want ErrInvalidCredentials", err)
		}
	})
}

func TestLoginSurfacesDatabaseFailure(t *testing.T) {
	service, mock := mockService(t)
	mock.ExpectQuery(`SELECT \* FROM "users"`).WillReturnError(errors.New("connection reset"))

	_, err := service.Login(context.Background(), "customer@mkp.test", password)
	if err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("error = %v, want the database failure rather than a credentials error", err)
	}
}
