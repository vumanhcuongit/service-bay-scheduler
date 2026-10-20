package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
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

// Every write needs the transaction InTx puts in its context. Outside one, a lock would be
// released as soon as it was taken and a key claim would commit on its own, so each call must
// fail rather than quietly run on the pool.
func TestWritesNeedTransaction(t *testing.T) {
	r := repository.New(testdb.Pool(t))
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	calls := map[string]func(context.Context) error{
		"LockDealership": func(ctx context.Context) error { return r.LockDealership(ctx, "DL1") },
		"LockVehicle":    func(ctx context.Context) error { return r.LockVehicle(ctx, "V1") },
		"ClaimKey": func(ctx context.Context) error {
			_, err := r.ClaimKey(ctx, "key", "hash", now)
			return err
		},
		"SaveKeyOutcome": func(ctx context.Context) error {
			return r.SaveKeyOutcome(ctx, "key", model.Outcome{})
		},
		"InsertAppointment": func(ctx context.Context) error {
			_, err := r.InsertAppointment(ctx, model.Appointment{})
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call(context.Background())
			if err == nil || !strings.Contains(err.Error(), "no transaction in context") {
				t.Errorf("outside InTx = %v, want a no-transaction error", err)
			}
		})
	}
	// Inside InTx the same ctx works, here for the two that need no setup.
	for _, name := range []string{"LockDealership", "LockVehicle"} {
		if err := r.InTx(context.Background(), calls[name]); err != nil {
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
