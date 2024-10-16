package migrate

import (
	"embed"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	log "github.com/sirupsen/logrus"
)

//go:embed migrations/*.sql
var fs embed.FS

func Migrate() {
	log.Info("Starting database migration")
	d, err := iofs.New(fs, "migrations")

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Error("Missing DATABASE_URL environment variable")
		os.Exit(1)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, connStr)
	if err != nil {
		log.WithError(err).Error("Failed to read migrations")
		os.Exit(1)
	}
	defer m.Close()
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.WithError(err).Error("Failed to apply migrations")
		os.Exit(1)
	}

	log.Info("Successfully applied migrations")
}
