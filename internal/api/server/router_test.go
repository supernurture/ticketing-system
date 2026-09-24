package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"ticketing-system/internal/config"
	"ticketing-system/internal/container"
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

func TestNewRouterServesHealth(t *testing.T) {
	rec := do(newTestRouter(t, testConfig(), newTestDeps(t)), http.MethodGet, "/health", "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID header missing, middleware chain did not run")
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

func TestNewRouterRejectsBadTrustedProxy(t *testing.T) {
	cfg := testConfig()
	cfg.Server.TrustedProxies = []string{"not-an-ip"}

	if _, err := NewRouter(cfg, newTestDeps(t)); err == nil {
		t.Fatal("NewRouter accepted an invalid trusted proxy")
	}
}
