package main

import (
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.Info("Starting database migration")
	pat := os.Getenv("DATABASE_MIGRATIONS")
	if pat == "" {
		pat = "file:///app/db/migrations"
	}
	log.WithField("location", pat).Debug("Migrations location")

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Error("Missing DATABASE_URL environment variable")
		os.Exit(1)
	}

	m, err := migrate.New(pat, connStr)
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
