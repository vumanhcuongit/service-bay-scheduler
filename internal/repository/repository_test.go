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

// Inserts at one dealership queue on its lock, so the one deadlock left is the same vehicle
// booked at two dealerships at once: each insert waits on the other's uncommitted row.
// Postgres aborts one, and it must reach the service as model.ErrDeadlock (a lost race),
// not as a 500.
func TestInsertAppointmentMapsDeadlock(t *testing.T) {
	r := repository.New(testdb.Pool(t))
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
		errs <- r.InTx(ctx, func(ctx context.Context) error {
			if _, err := r.InsertAppointment(ctx, first); err != nil {
				return err
			}
			close(mine)
			<-theirs
			_, err := r.InsertAppointment(ctx, second) // waits on the other's first row
			return err
		})
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
