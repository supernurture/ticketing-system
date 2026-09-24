package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"ticketing-system/internal/config"
	"ticketing-system/internal/container"
	"ticketing-system/internal/middleware"
	"ticketing-system/pkg/logger"
)

const testSecret = "a-test-secret-that-is-at-least-32-chars"

func testConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Server.Mode = gin.TestMode
	cfg.Server.Timeout = time.Second
	cfg.Auth.JWTSecret = testSecret
	cfg.Auth.TokenTTL = time.Hour
	return cfg
}

func newTestDeps(t *testing.T) *container.Container {
	t.Helper()
	log, err := logger.New(logger.Config{ServiceName: "test", Path: t.TempDir()})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })

	return &container.Container{Logger: log}
}

func withPostgres(t *testing.T, deps *container.Container) (*container.Container, sqlmock.Sqlmock) {
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

	deps.Postgres = map[string]*gorm.DB{"ticketing": db}
	return deps, mock
}

func newTestRouter(t *testing.T, cfg *config.Config, deps *container.Container) *gin.Engine {
	t.Helper()
	router, err := NewRouter(cfg, deps)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return router
}

func token(t *testing.T, role string) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, middleware.Claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "1",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

func do(router *gin.Engine, method, path, bearer, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

const showtimeBody = `{"movie_id":1,"studio_id":1,"start_at":"2099-01-01T19:00:00+07:00","price":50000}`

func TestNewRouterServesHealth(t *testing.T) {
	rec := do(newTestRouter(t, testConfig(), newTestDeps(t)), http.MethodGet, "/health", "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header missing, middleware chain did not run")
	}
}

func TestShowtimeRoutesNeedAToken(t *testing.T) {
	deps, _ := withPostgres(t, newTestDeps(t))
	router := newTestRouter(t, testConfig(), deps)

	tests := map[string]struct {
		bearer string
		want   int
	}{
		"no token":            {"", http.StatusUnauthorized},
		"garbage token":       {"not-a-jwt", http.StatusUnauthorized},
		"customer writes":     {token(t, "customer"), http.StatusForbidden},
		"admin, bad query id": {token(t, middleware.RoleAdmin), http.StatusBadRequest},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			method, path := http.MethodPost, "/api/v1/showtimes"
			if strings.HasPrefix(name, "admin") {
				method, path = http.MethodGet, "/api/v1/showtimes/abc"
			}
			if rec := do(router, method, path, tc.bearer, showtimeBody); rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// The overlap check lives in the database (EXCLUDE constraint), so it holds even for two concurrent requests;
// the API only has to turn the violation into a 409.
func TestCreateOverlappingShowtimeIs409(t *testing.T) {
	deps, mock := withPostgres(t, newTestDeps(t))
	mock.ExpectQuery(`SELECT "duration_minutes" FROM "movies"`).
		WillReturnRows(sqlmock.NewRows([]string{"duration_minutes"}).AddRow(120))
	mock.ExpectQuery(`SELECT count\(\*\) FROM "studios"`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO "showtimes"`).
		WillReturnError(&pgconn.PgError{Code: "23P01", ConstraintName: "showtimes_no_overlap"})
	mock.ExpectRollback()

	rec := do(newTestRouter(t, testConfig(), deps),
		http.MethodPost, "/api/v1/showtimes", token(t, middleware.RoleAdmin), showtimeBody)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusConflict, rec.Body)
	}
	want := `{"message":"overlaps another showtime in this studio, including 15 minutes of cleaning"}`
	if got := rec.Body.String(); !strings.HasPrefix(got, want) {
		t.Errorf("body = %q, want %q", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestLoginRejectsBadBodies(t *testing.T) {
	deps, _ := withPostgres(t, newTestDeps(t))
	cfg := testConfig()
	cfg.Server.MaxBodyBytes = 64
	router := newTestRouter(t, cfg, deps)

	tests := map[string]struct {
		body string
		want int
	}{
		"empty body":       {"", http.StatusBadRequest},
		"missing password": {`{"email":"a@b.c"}`, http.StatusBadRequest},
		"oversized body":   {`{"email":"` + strings.Repeat("x", 128) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if rec := do(router, http.MethodPost, "/api/v1/auth/login", "", tc.body); rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// Failed logins and admin changes are written to the application log with the caller's request ID,
// so they can be matched with the access-log line of the same request.
func TestFailedLoginAndAdminChangeAreLogged(t *testing.T) {
	logDir := t.TempDir()
	log, err := logger.New(logger.Config{ServiceName: "test", Path: logDir})
	if err != nil {
		t.Fatalf("logger.New: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })
	deps, mock := withPostgres(t, &container.Container{Logger: log})
	router := newTestRouter(t, testConfig(), deps)

	send := func(method, path, requestID, bearer, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", requestID)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	mock.ExpectQuery(`SELECT \* FROM "users"`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "role"}))
	if code := send(http.MethodPost, "/api/v1/auth/login", "req-login-1", "",
		`{"email":" Nobody@MKP.test ","password":"guess-123"}`); code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want 401", code)
	}

	mock.ExpectBegin()
	mock.ExpectExec(`DELETE FROM "showtimes"`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if code := send(http.MethodDelete, "/api/v1/showtimes/9", "req-delete-1",
		token(t, middleware.RoleAdmin), ""); code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", code)
	}

	files, _ := filepath.Glob(filepath.Join(logDir, "test", "*.log"))
	var logged strings.Builder
	for _, name := range files {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		logged.Write(contents)
	}
	out := logged.String()

	// The access log carries the request ID too, so each audit line is checked on its own.
	lineWith := func(msg string) string {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, `"`+msg+`"`) {
				return line
			}
		}
		t.Fatalf("no %q line in the log\n%s", msg, out)
		return ""
	}
	checks := map[string][]string{
		"login failed":     {`"request_id":"req-login-1"`, `"email":"n***@mkp.test"`, `"reason":"unknown_email"`},
		"showtime deleted": {`"request_id":"req-delete-1"`, `"user_id":"1"`, `"showtime_id":9`},
	}
	for msg, wants := range checks {
		line := lineWith(msg)
		for _, want := range wants {
			if !strings.Contains(line, want) {
				t.Errorf("%q line is missing %s: %s", msg, want, line)
			}
		}
	}
	if strings.Contains(out, "guess-123") {
		t.Error("the attempted password was written to the log")
	}
}

func TestNewRouterRejectsBadTrustedProxy(t *testing.T) {
	cfg := testConfig()
	cfg.Server.TrustedProxies = []string{"not-an-ip"}

	if _, err := NewRouter(cfg, newTestDeps(t)); err == nil {
		t.Fatal("NewRouter accepted an invalid trusted proxy")
	}
}
