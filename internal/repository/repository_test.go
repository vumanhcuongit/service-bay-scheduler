package repository_test

import (
	"context"
	"testing"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/repository"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/testdb"
)

// A panic inside fn must still end the transaction, or its pool connection
// (and any row lock it holds) is never released.
func TestInTxReleasesConnectionOnPanic(t *testing.T) {
	pool := testdb.Pool(t)
	repo := repository.New(pool)

	func() {
		defer func() { _ = recover() }()
		_ = repo.InTx(context.Background(), func(ctx context.Context) error { panic("boom") })
	}()

	if n := pool.Stat().AcquiredConns(); n != 0 {
		t.Fatalf("%d connection(s) still acquired after a panic in InTx", n)
	}
}

// Outside a transaction an advisory lock would be released as soon as it was taken, so the
// call must fail rather than silently not lock.
func TestLocksNeedTransaction(t *testing.T) {
	r := repository.New(testdb.Pool(t))
	locks := map[string]func(context.Context) error{
		"LockDealership": func(ctx context.Context) error { return r.LockDealership(ctx, "DL1") },
		"LockVehicle":    func(ctx context.Context) error { return r.LockVehicle(ctx, "V1") },
	}
	for name, lock := range locks {
		if err := lock(context.Background()); err == nil {
			t.Errorf("%s outside InTx returned nil, want an error", name)
		}
		if err := r.InTx(context.Background(), lock); err != nil {
			t.Errorf("%s inside InTx: %v", name, err)
		}
	}
}

// Behind a transaction-mode pooler, the next client of a connection never sees a statement a
// previous one prepared by name. After queries run twice (past any cache threshold) the
// connection holds no named prepared statement.
func TestConnectPreparesNoNamedStatements(t *testing.T) {
	ctx := context.Background()
	pool, err := repository.Connect(ctx, testdb.Pool(t).Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	for range 3 {
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM bays WHERE dealership_id = $1`, "DL1").Scan(&n); err != nil {
			t.Fatal(err)
		}
	}
	var named int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM pg_prepared_statements`).Scan(&named); err != nil {
		t.Fatal(err)
	}
	if named != 0 {
		t.Errorf("%d named prepared statement(s) on the connection, want 0", named)
	}
}
