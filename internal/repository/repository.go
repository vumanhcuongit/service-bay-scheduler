// Package repository is the Postgres implementation of service.Repository. It runs SQL,
// owns transactions, and turns constraint violations into model errors. It holds no business rules.
package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is satisfied by *pgxpool.Pool and by pgx.Tx, whose Begin opens a savepoint.
// Tests pass a pgx.Tx so every transaction nests inside one that is rolled back.
type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
	db DB
}

func New(db DB) *Repository { return &Repository{db: db} }

// Connect opens a pool whose statements time out after 5 s (D12).
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = "5000"
	return pgxpool.NewWithConfig(ctx, cfg)
}

// txKey carries the current transaction in the context, so the repository need not import
// the service package that declares its interface.
type txKey struct{}

// q returns the transaction in ctx, or the pool when there is none.
func (r *Repository) q(ctx context.Context) DB {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return r.db
}

// InTx is the only place a transaction starts. Inside an existing one it opens a savepoint.
func (r *Repository) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := r.q(ctx).Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Runs on error and on panic; after a successful Commit it is a no-op.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
