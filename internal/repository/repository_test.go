package repository_test

import (
	"context"
	"errors"
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

// A mapping test, not a reproduction: two inserts per transaction force a 40P01 on demand,
// which no booking does. The deadlock it stands for is the one LockDealership leaves, the same
// vehicle at two dealerships. Postgres aborts one, and it must reach the service as
// model.ErrDeadlock, which gets the re-run, not as a 500. The transactions are opened here, not
// by InTx: InTx's lock_timeout equals Postgres's default deadlock_timeout and fires first (D8).
func TestInsertAppointmentMapsDeadlock(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	at := func(dealership, serviceType, vehicle, bay, tech string, hour int) model.Appointment {
		start := time.Date(2026, 10, 21, hour, 0, 0, 0, time.UTC)
		return model.Appointment{
			DealershipID: dealership, CustomerID: "C1", VehicleID: vehicle, ServiceTypeID: serviceType,
			BayID: bay, TechnicianID: tech, Start: start, End: start.Add(30 * time.Minute),
			Status: model.StatusConfirmed,
		}
	}
	errs := make(chan error, 2)
	run := func(first, second model.Appointment, mine, theirs chan struct{}) {
		errs <- func() error {
			tx, err := pool.Begin(ctx)
			if err != nil {
				return err
			}
			defer func() { _ = tx.Rollback(ctx) }()
			r := repository.New(tx) // no lock_timeout, so the deadlock detector decides
			if _, err := r.InsertAppointment(ctx, first); err != nil {
				return err
			}
			close(mine)
			<-theirs
			if _, err := r.InsertAppointment(ctx, second); err != nil { // waits on the other's first row
				return err
			}
			return tx.Commit(ctx)
		}()
	}
	a, b := make(chan struct{}), make(chan struct{})
	go run(at("DL1", "OIL", "V1", "B1", "T1", 2), at("DL1", "OIL", "V2", "B2", "T2", 3), a, b)
	go run(at("DL2", "TIRE", "V2", "B3", "T4", 3), at("DL2", "TIRE", "V1", "B3", "T4", 2), b, a)

	var deadlocked, ok int
	for range 2 {
		switch err := <-errs; {
		case err == nil:
			ok++
		case errors.Is(err, model.ErrDeadlock):
			deadlocked++
		default:
			t.Errorf("err = %v, want nil or model.ErrDeadlock", err)
		}
	}
	if ok != 1 || deadlocked != 1 {
		t.Errorf("%d committed, %d deadlocked; want 1 and 1", ok, deadlocked)
	}
}

// Outside a transaction the advisory lock would be released as soon as it was taken, so the
// call must fail rather than silently not lock.
func TestLockDealershipNeedsTransaction(t *testing.T) {
	r := repository.New(testdb.Pool(t))
	if err := r.LockDealership(context.Background(), "DL1"); err == nil {
		t.Fatal("LockDealership outside InTx returned nil, want an error")
	}
	if err := r.InTx(context.Background(), func(ctx context.Context) error {
		return r.LockDealership(ctx, "DL1")
	}); err != nil {
		t.Fatalf("LockDealership inside InTx: %v", err)
	}
}
