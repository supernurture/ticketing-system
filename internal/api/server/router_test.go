package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"ticketing-system/internal/config"
	"ticketing-system/internal/container"
	"ticketing-system/pkg/logger"
)

func testConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Server.Mode = gin.TestMode
	cfg.Server.Timeout = time.Second
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

func TestNewRouterRejectsBadTrustedProxy(t *testing.T) {
	cfg := testConfig()
	cfg.Server.TrustedProxies = []string{"not-an-ip"}

	if _, err := NewRouter(cfg, newTestDeps(t)); err == nil {
		t.Fatal("NewRouter accepted an invalid trusted proxy")
	}
}
