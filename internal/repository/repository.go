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

// Connect opens a pool that is safe behind a transaction-mode pooler (design §8): it sets no
// session state, and it names no prepared statement. pgx's default exec mode prepares a named
// statement per connection, which another client's transaction on a pooled connection would not
// find; CacheDescribe caches only the statement's description and executes it unnamed.
// DB calls are bounded by the request deadline instead of a startup parameter (D12).
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
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

// lockTimeout bounds each lock wait in a transaction, so a wait gives a 503 instead of running
// into the request deadline (D8). It is right for the key row and the dealership lock, which
// cannot deadlock: the key always comes first. A wait that can deadlock must allow longer than
// Postgres's deadlock_timeout (1 s by default, superuser-only), or the detector never picks a
// victim and both sides time out; InsertAppointment raises it to insertLockTimeout.
const (
	lockTimeout       = "1s"
	insertLockTimeout = "2s"
)

// InTx is the only place a transaction starts. Inside an existing one it opens a savepoint.
// Its first statement sets lock_timeout, before any call in fn can wait on a lock.
func (r *Repository) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := r.q(ctx).Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Runs on error and on panic; after a successful Commit it is a no-op.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '`+lockTimeout+`'`); err != nil {
		return fmt.Errorf("set lock_timeout: %w", err)
	}
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
