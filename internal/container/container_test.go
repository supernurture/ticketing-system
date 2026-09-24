package container

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"ticketing-system/internal/config"
	"ticketing-system/pkg/database"
)

func stubOpeners(t *testing.T) {
	t.Helper()

	openMock := func(string, int, string, string, string, string, database.PoolConfig) (*gorm.DB, error) {
		sqlDB, mock, err := sqlmock.New()
		if err != nil {
			return nil, err
		}
		mock.ExpectClose()
		return gorm.Open(
			postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
			&gorm.Config{DisableAutomaticPing: true},
		)
	}

	postgresOrig := newPostgres
	newPostgres = openMock
	t.Cleanup(func() { newPostgres = postgresOrig })
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := &config.Config{}
	cfg.App.Name = "test"
	cfg.Logger.Path = t.TempDir()
	return cfg
}

func TestNewContainerWithoutDependencies(t *testing.T) {
	c, err := NewContainer(testConfig(t))
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}
	if c.Logger == nil {
		t.Error("logger should always be built")
	}
	if len(c.Postgres) != 0 {
		t.Errorf("unconfigured dependencies should be absent: %+v", c)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestCloseUnwindsInReverse(t *testing.T) {
	var order []string
	c := &Container{shutdowns: []func() error{
		func() error { order = append(order, "logger"); return nil },
		func() error { order = append(order, "postgres"); return errors.New("boom") },
		func() error { order = append(order, "cache"); return nil },
	}}

	err := c.Close()

	if got, want := strings.Join(order, ","), "cache,postgres,logger"; got != want {
		t.Errorf("shutdown order = %q, want %q", got, want)
	}
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("Close error = %v, want it to carry the hook failure", err)
	}
}

func TestNewContainerOpensEveryDependency(t *testing.T) {
	stubOpeners(t)

	cfg := testConfig(t)
	cfg.Databases.Postgres = map[string]config.Postgres{"primary": {}}

	c, err := NewContainer(cfg)
	if err != nil {
		t.Fatalf("NewContainer: %v", err)
	}

	if c.Postgres["primary"] == nil {
		t.Errorf("not every dependency was stored: %+v", c)
	}
	if len(c.shutdowns) != 2 {
		t.Errorf("shutdown hooks = %d, want 2", len(c.shutdowns))
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNewContainerNamesTheFailingDependency(t *testing.T) {
	tests := map[string]func(cfg *config.Config){
		`postgres "primary"`: func(cfg *config.Config) {
			cfg.Databases.Postgres = map[string]config.Postgres{
				"primary": {Host: "127.0.0.1", Port: 2, Opts: "connect_timeout=1"},
			}
		},
	}

	for want, configure := range tests {
		t.Run(want, func(t *testing.T) {
			cfg := testConfig(t)
			configure(cfg)

			_, err := NewContainer(cfg)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want it to name %s", err, want)
			}
		})
	}
}

func TestNewContainerReportsLoggerFailure(t *testing.T) {
	cfg := testConfig(t)
	blocked := filepath.Join(cfg.Logger.Path, cfg.App.Name)
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write %s: %v", blocked, err)
	}

	if _, err := NewContainer(cfg); err == nil || !strings.Contains(err.Error(), "build logger") {
		t.Fatalf("error = %v, want a logger failure", err)
	}
}

func TestCloseGorm(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	mock.ExpectClose()
	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}),
		&gorm.Config{DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatalf("gorm.Open: %v", err)
	}

	if err := closeGorm(db)(); err != nil {
		t.Errorf("closeGorm: %v", err)
	}

	if err := closeGorm(&gorm.DB{Config: &gorm.Config{}})(); err == nil {
		t.Error("expected an error from a connection with no pool")
	}
}
