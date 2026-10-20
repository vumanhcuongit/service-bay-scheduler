package spectest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/repository"
)

// make races runs the pool tests 20 times in a row, in make ci and in CI, so a pass is not luck.

func TestM17(t *testing.T) {
	e := newPoolEnv(t)
	b := func() *httptest.ResponseRecorder { return e.book("V1", "OIL", "09:00", key(K3)) }
	rs := together(b, b)
	e.created(rs[0], "B1", "T1")
	e.created(rs[1], "B1", "T1")
	expect(t, "replayed body", rs[1].Body.String(), rs[0].Body.String())
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 1)
}

func TestM25(t *testing.T) {
	e := newPoolEnv(t)
	rs := together(
		func() *httptest.ResponseRecorder { return e.book("V1", "OIL", "09:00") },
		func() *httptest.ResponseRecorder { return e.book("V2", "OIL", "09:00") },
	)
	got := []string{e.assigned(rs[0]), e.assigned(rs[1])}
	slices.Sort(got)
	expect(t, "assignments", fmt.Sprint(got), "[B1/T1 B2/T2]")
}

func TestN36(t *testing.T) {
	e := newPoolEnv(t)
	rs := together(
		func() *httptest.ResponseRecorder { return e.book("V1", "BRAKE", "09:00") },
		func() *httptest.ResponseRecorder { return e.book("V2", "BRAKE", "09:00") },
	)
	won, lost := winnerFirst(rs)
	e.created(won, "B1", "T1")
	e.conflict(lost, "no_technician")
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 1)
}

func TestN37(t *testing.T) {
	e := newPoolEnv(t)
	rs := together(
		func() *httptest.ResponseRecorder { return e.book("V1", "OIL", "09:00") },
		func() *httptest.ResponseRecorder { return e.book("V1", "BRAKE", "09:00") },
	)
	won, lost := winnerFirst(rs)
	e.status(won, http.StatusCreated)
	e.conflict(lost, "vehicle_busy")
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 1)
}

// Ten bookings race for the two OIL slots at 09:00. The dealership lock runs them one at a
// time, so exactly two win, one per bay, and every loser's check sees both: 409 no_bay, never 503.
func TestTenRacersForTwoSlots(t *testing.T) {
	e := newPoolEnv(t)
	extraVehicles := []string{"V4", "V5", "V6", "V7", "V8", "V9", "V10"}
	ctx := context.Background()
	for _, id := range extraVehicles {
		if _, err := e.db.Exec(ctx, `INSERT INTO vehicles (id, customer_id) VALUES ($1, 'C1') ON CONFLICT DO NOTHING`, id); err != nil {
			t.Fatal(err)
		}
	}
	// Runs before the pool's truncate, so the vehicles' appointments go first.
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM appointments WHERE vehicle_id = ANY($1)`, `DELETE FROM vehicles WHERE id = ANY($1)`} {
			if _, err := e.db.Exec(ctx, q, extraVehicles); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	var bookings []func() *httptest.ResponseRecorder
	for _, v := range append([]string{"V1", "V2", "V3"}, extraVehicles...) {
		bookings = append(bookings, func() *httptest.ResponseRecorder { return e.book(v, "OIL", "09:00") })
	}
	var won []string
	for _, r := range together(bookings...) {
		if r.Code == http.StatusCreated {
			won = append(won, e.assigned(r))
		} else {
			e.conflict(r, "no_bay")
		}
	}
	slices.Sort(won)
	expect(t, "winners", fmt.Sprint(won), "[B1/T1 B2/T2]")
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 2)
}

func TestN38(t *testing.T) {
	f := &flakyRepo{errs: always(model.ErrBayTaken)}
	e := newEnvWith(t, f.wrap)
	r := e.book("V1", "OIL", "09:00", key(K5))
	e.status(r, http.StatusServiceUnavailable)
	expect(t, "Retry-After", r.Header().Get("Retry-After"), "1")
	expect(t, "inserts", f.inserts, 2)
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 0)
	expect(t, "key rows", e.count("SELECT count(*) FROM idempotency_keys WHERE key = $1", K5), 0)
}

// The re-run's own result decides: a vehicle violation is the only one that becomes a 409.
func TestRerunOutcome(t *testing.T) {
	for _, c := range []struct {
		name   string
		errs   []error
		status int
	}{
		{"deadlock on re-run", []error{model.ErrBayTaken, model.ErrDeadlock}, http.StatusServiceUnavailable},
		{"vehicle then bay", []error{model.ErrVehicleBusy, model.ErrBayTaken}, http.StatusServiceUnavailable},
		{"deadlock then vehicle", []error{model.ErrDeadlock, model.ErrVehicleBusy}, http.StatusConflict},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &flakyRepo{errs: c.errs}
			e := newEnvWith(t, f.wrap)
			r := e.book("V1", "OIL", "09:00", key(K5))
			if c.status == http.StatusConflict {
				e.conflict(r, "vehicle_busy")
			} else {
				e.status(r, c.status)
				expect(t, "Retry-After", r.Header().Get("Retry-After"), "1")
			}
			expect(t, "inserts", f.inserts, 2)
			expect(t, "key rows", e.count("SELECT count(*) FROM idempotency_keys WHERE key = $1", K5), 0)
		})
	}
}

// The same vehicle at two dealerships at once: each holds only its own dealership's lock, so
// this is the race the lock cannot cover. The vehicle constraint and the re-run make it a 409.
func TestVehicleAtTwoDealerships(t *testing.T) {
	e := newPoolEnv(t)
	rs := together(
		func() *httptest.ResponseRecorder { return e.book("V1", "OIL", "09:00") },
		func() *httptest.ResponseRecorder { return e.book("V1", "TIRE", "09:00", dl2()) },
	)
	won, lost := winnerFirst(rs)
	e.status(won, http.StatusCreated)
	e.conflict(lost, "vehicle_busy")
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 1)
}

// A booking that waits on the dealership lock past lock_timeout gets a 503, never a 500.
// The lock is held by another session, as a booking at the same dealership would hold it.
func TestLockTimeoutIs503(t *testing.T) {
	e := newPoolEnv(t)
	r := repository.New(e.db)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- r.InTx(context.Background(), func(ctx context.Context) error {
			if err := r.LockDealership(ctx, "DL1"); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	rec := e.book("V1", "OIL", "09:00", key(K5))
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.status(rec, http.StatusServiceUnavailable)
	expect(t, "Retry-After", rec.Header().Get("Retry-After"), "1")
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 0)
	expect(t, "key rows", e.count("SELECT count(*) FROM idempotency_keys WHERE key = $1", K5), 0)
}

func TestVehicleViolationTwiceIs409(t *testing.T) {
	f := &flakyRepo{errs: always(model.ErrVehicleBusy)}
	e := newEnvWith(t, f.wrap)
	e.conflict(e.book("V1", "OIL", "09:00", key(K5)), "vehicle_busy")
	expect(t, "inserts", f.inserts, 2)
	expect(t, "key rows", e.count("SELECT count(*) FROM idempotency_keys WHERE key = $1", K5), 0)
}

// A constraint violation or deadlock re-runs in a fresh transaction that claims the key again,
// so the 201 is stored once and replays.
func TestLostRaceRetriesWithKey(t *testing.T) {
	for _, lost := range []error{model.ErrBayTaken, model.ErrTechnicianTaken, model.ErrDeadlock} {
		t.Run(lost.Error(), func(t *testing.T) {
			f := &flakyRepo{errs: []error{lost}}
			e := newEnvWith(t, f.wrap)
			r1 := e.book("V1", "OIL", "09:00", key(K5))
			e.created(r1, "B1", "T1")
			expect(t, "failed inserts", f.inserts, 1)
			expect(t, "created key rows", e.count("SELECT count(*) FROM idempotency_keys WHERE key = $1 AND status = 'created'", K5), 1)
			r2 := e.book("V1", "OIL", "09:00", key(K5))
			expect(t, "replayed body", r2.Body.String(), r1.Body.String())
		})
	}
}

// The usual vehicle violation: the same vehicle booked at DL2 has committed by the re-run, so
// its fresh check returns the 409, which is stored and replays.
func TestVehicleViolationRerunStores409(t *testing.T) {
	f := &flakyRepo{errs: []error{model.ErrVehicleBusy}}
	e := newPoolEnvWith(t, f.wrap)
	f.onInsert = func() {
		if f.inserts == 1 {
			e.given("V1", "TIRE", "09:00", "B3", "T4") // DL2's row, committed on its own connection
		}
	}
	r1 := e.book("V1", "OIL", "09:00", key(K5))
	e.conflict(r1, "vehicle_busy")
	expect(t, "conflict key rows", e.count("SELECT count(*) FROM idempotency_keys WHERE key = $1 AND status = 'conflict'", K5), 1)
	r2 := e.book("V1", "OIL", "09:00", key(K5))
	expect(t, "replayed body", r2.Body.String(), r1.Body.String())
}

// A request cancelled during the backoff stops waiting at once and does not try again.
func TestBackoffStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var cancelledAt time.Time
	f := &flakyRepo{errs: always(model.ErrBayTaken)}
	f.onInsert = func() {
		if f.inserts == 1 {
			cancelledAt = time.Now()
			cancel()
		}
	}
	e := newEnvWith(t, f.wrap)
	_, err := e.svc.Book(ctx, model.BookingRequest{
		DealershipID: "DL1", VehicleID: "V1", ServiceTypeID: "OIL",
		Start: time.Date(2026, 10, 21, 9, 0, 0, 0, hcm),
	}, "")
	elapsed := time.Since(cancelledAt)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if elapsed >= 50*time.Millisecond {
		t.Errorf("returned %v after cancel, want < 50ms", elapsed)
	}
	expect(t, "inserts", f.inserts, 1)
}

// assigned asserts a 201 and returns its "bay/technician".
func (e *env) assigned(r *httptest.ResponseRecorder) string {
	e.t.Helper()
	e.status(r, http.StatusCreated)
	var a appointment
	if err := json.Unmarshal(r.Body.Bytes(), &a); err != nil {
		e.t.Fatalf("decode 201 body %q: %v", r.Body.String(), err)
	}
	return a.BayID + "/" + a.TechnicianID
}

// winnerFirst orders two responses so the 201, if any, comes first.
func winnerFirst(rs []*httptest.ResponseRecorder) (won, lost *httptest.ResponseRecorder) {
	if rs[1].Code == http.StatusCreated {
		return rs[1], rs[0]
	}
	return rs[0], rs[1]
}
