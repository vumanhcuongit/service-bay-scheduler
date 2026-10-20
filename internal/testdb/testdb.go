// Package testdb gives tests a migrated Postgres: a transaction rolled back after
// the test, or the shared pool for tests that need real commits.
package testdb

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/vumanhcuongit/service-bay-scheduler/migrations"
)

const defaultURL = "postgres://scheduler:scheduler@localhost:5432/scheduler_test?sslmode=disable"

var (
	once    sync.Once
	pool    *pgxpool.Pool
	initErr error
)

func shared(t *testing.T) *pgxpool.Pool {
	t.Helper()
	once.Do(func() {
		url := os.Getenv("TEST_DATABASE_URL")
		if url == "" {
			url = defaultURL
		}
		ctx := context.Background()
		pool, initErr = pgxpool.New(ctx, url)
		if initErr != nil {
			return
		}
		db := stdlib.OpenDBFromPool(pool)
		defer db.Close()
		var p *goose.Provider
		p, initErr = goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
		if initErr != nil {
			return
		}
		_, initErr = p.Up(ctx)
	})
	if initErr != nil {
		t.Fatalf("testdb: %v (is `make db-up` running?)", initErr)
	}
	return pool
}

// Tx returns a transaction that is rolled back when the test ends.
func Tx(t *testing.T) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := shared(t).Begin(ctx)
	if err != nil {
		t.Fatalf("testdb: begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

// Pool returns the shared pool for tests whose transactions must really commit and race.
// Bookings and keys are truncated when the test ends; seed data stays.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p := shared(t)
	t.Cleanup(func() {
		if _, err := p.Exec(context.Background(), "TRUNCATE appointments, idempotency_keys"); err != nil {
			t.Errorf("testdb: truncate: %v", err)
		}
	})
	return p
}
