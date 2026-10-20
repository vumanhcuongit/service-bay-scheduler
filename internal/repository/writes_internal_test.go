package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/testdb"
)

// A mapping test, not a reproduction: two inserts per transaction force a 40P01 on demand,
// which no booking does now that it takes the dealership and vehicle locks. Postgres aborts
// one, and it must reach the service as model.ErrDeadlock, not as a 500. Each transaction
// raises its lock_timeout above deadlock_timeout first, or the 1 s that InTx sets would end
// both waits before the detector picks a victim.
func TestInsertAppointmentMapsDeadlock(t *testing.T) {
	r := New(testdb.Pool(t))
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
			if _, err := ctx.Value(txKey{}).(pgx.Tx).Exec(ctx, `SET LOCAL lock_timeout = '5s'`); err != nil {
				return err
			}
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
