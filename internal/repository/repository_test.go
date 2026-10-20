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
