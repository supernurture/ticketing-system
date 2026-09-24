package container

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"ticketing-system/internal/config"
	"ticketing-system/pkg/database"
	"ticketing-system/pkg/logger"
)

// Container holds shared dependencies, keyed by their config name.
type Container struct {
	Logger   *logger.Logger
	Postgres map[string]*gorm.DB

	shutdowns []func() error
}

// NewContainer opens every configured dependency. Call Close when done.
func NewContainer(cfg *config.Config) (*Container, error) {
	log, err := newLogger(cfg)
	if err != nil {
		return nil, err
	}

	deps := &Container{
		Logger:   log,
		Postgres: make(map[string]*gorm.DB, len(cfg.Databases.Postgres)),

		shutdowns: []func() error{log.Close},
	}

	if err := deps.open(cfg); err != nil {
		_ = deps.Close()
		return nil, err
	}

	return deps, nil
}

// Close unwinds every hook in reverse, continuing past failures.
func (c *Container) Close() error {
	var errs []error
	for x := len(c.shutdowns) - 1; x >= 0; x-- {
		errs = append(errs, c.shutdowns[x]())
	}

	return errors.Join(errs...)
}

var newPostgres = database.NewPostgres

func (c *Container) open(cfg *config.Config) error {
	for name, db := range cfg.Databases.Postgres {
		conn, err := newPostgres(db.Host, db.Port, db.User, db.Password, db.Database, db.Opts, database.PoolConfig{
			MaxOpenConns:    db.MaxOpenConns,
			MaxIdleConns:    db.MaxIdleConns,
			ConnMaxLifetime: db.ConnMaxLifetime,
		})
		if err != nil {
			return fmt.Errorf("open postgres %q: %w", name, err)
		}
		c.Postgres[name] = conn
		c.shutdowns = append(c.shutdowns, closeGorm(conn))
	}

	return nil
}

func newLogger(cfg *config.Config) (*logger.Logger, error) {
	log, err := logger.New(logger.Config{
		ServiceName: cfg.App.Name,
		Env:         cfg.App.Env,
		Path:        cfg.Logger.Path,
		Level:       cfg.Logger.Level,
		Console:     cfg.Logger.Console,
		Rotation: logger.RotationOptions{
			Daily:      cfg.Logger.RotationPattern == "daily",
			MaxSizeMB:  cfg.Logger.RotationSizeMB,
			MaxAgeDays: cfg.Logger.RetentionDays,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	return log, nil
}

func closeGorm(conn *gorm.DB) func() error {
	return func() error {
		sqlDB, err := conn.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
}
