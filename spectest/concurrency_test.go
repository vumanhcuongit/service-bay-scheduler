package spectest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/handler"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/repository"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/service"
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

// deadlockCounter counts the inserts a booking makes and the ones Postgres aborts as deadlocks.
// A booking re-runs after a deadlock, so the status codes alone cannot tell a run that
// deadlocked and recovered from one that never did.
type deadlockCounter struct {
	service.Repository
	inserts, deadlocks atomic.Int32
}

func (c *deadlockCounter) wrap(r service.Repository) service.Repository {
	c.Repository = r
	return c
}

func (c *deadlockCounter) InsertAppointment(ctx context.Context, a model.Appointment) (model.Appointment, error) {
	c.inserts.Add(1)
	a, err := c.Repository.InsertAppointment(ctx, a)
	if errors.Is(err, model.ErrDeadlock) {
		c.deadlocks.Add(1)
	}
	return a, err
}

// The same vehicle at two dealerships at once: each holds only its own dealership's lock, so
// the vehicle lock is what serialises them. The loser reads the winner's committed row and gets
// the 409 from its check, never from the vehicle constraint, and Postgres never has to break a
// deadlock between them. The re-run would hide one, so the deadlocks are counted in the
// repository. Each round is a new race; make races repeats the test.
func TestVehicleAtTwoDealerships(t *testing.T) {
	c := &deadlockCounter{}
	e := newPoolEnvWith(t, c.wrap)
	const rounds = 5
	for i := range rounds {
		at := fmt.Sprintf("%02d:00", 9+i)
		rs := together(
			func() *httptest.ResponseRecorder { return e.book("V1", "OIL", at) },
			func() *httptest.ResponseRecorder { return e.book("V1", "TIRE", at, dl2()) },
		)
		won, lost := winnerFirst(rs)
		e.status(won, http.StatusCreated)
		e.conflict(lost, "vehicle_busy")
	}
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), rounds)
	expect(t, "deadlocks (40P01)", c.deadlocks.Load(), 0)
	expect(t, "inserts", c.inserts.Load(), rounds)
}

// noLockRepo turns both advisory locks into no-ops, so only the exclusion constraints stand
// between a race and a double booking.
type noLockRepo struct{ service.Repository }

func noLocks(r service.Repository) service.Repository { return noLockRepo{r} }

func (noLockRepo) LockDealership(context.Context, string) error { return nil }

func (noLockRepo) LockVehicle(context.Context, string) error { return nil }

// With the locks gone, the bookings of M25, N36 and N37, and one vehicle at two dealerships, still never leave two confirmed rows
// that overlap on one bay, technician or vehicle, and never fail with a 500. This is what
// shows the constraints do the work: with them dropped, the same races double-book. The lock
// is for liveness; a race without it may be a 201, a 409 or a 503, in any mix. make races
// repeats it, so a pass is not luck. Without locks every racer picks the same first bay and
// technician, so the bay and technician constraints are proven together, not one by one: drop
// only one and the other still catches it. The vehicle constraint is proven alone, by the
// two-dealership scenario.
func TestConstraintsHoldWithoutLocks(t *testing.T) {
	scenarios := map[string]func(e *env) []func() *httptest.ResponseRecorder{
		"M25": func(e *env) []func() *httptest.ResponseRecorder {
			return []func() *httptest.ResponseRecorder{
				func() *httptest.ResponseRecorder { return e.book("V1", "OIL", "09:00") },
				func() *httptest.ResponseRecorder { return e.book("V2", "OIL", "09:00") },
			}
		},
		"N36": func(e *env) []func() *httptest.ResponseRecorder {
			return []func() *httptest.ResponseRecorder{
				func() *httptest.ResponseRecorder { return e.book("V1", "BRAKE", "09:00") },
				func() *httptest.ResponseRecorder { return e.book("V2", "BRAKE", "09:00") },
			}
		},
		"N37": func(e *env) []func() *httptest.ResponseRecorder {
			return []func() *httptest.ResponseRecorder{
				func() *httptest.ResponseRecorder { return e.book("V1", "OIL", "09:00") },
				func() *httptest.ResponseRecorder { return e.book("V1", "BRAKE", "09:00") },
			}
		},
		// N37 books the same bay and technician too, so those constraints would catch it with the
		// vehicle one gone. Two dealerships share neither, so only the vehicle constraint is left.
		"vehicle at two dealerships": func(e *env) []func() *httptest.ResponseRecorder {
			return []func() *httptest.ResponseRecorder{
				func() *httptest.ResponseRecorder { return e.book("V1", "OIL", "09:00") },
				func() *httptest.ResponseRecorder { return e.book("V1", "TIRE", "09:00", dl2()) },
			}
		},
	}
	for name, bookings := range scenarios {
		t.Run(name, func(t *testing.T) {
			e := newPoolEnvWith(t, noLocks)
			for _, r := range together(bookings(e)...) {
				switch r.Code {
				case http.StatusCreated, http.StatusConflict, http.StatusServiceUnavailable:
				default:
					t.Fatalf("status %d, want 201, 409 or 503; body %s", r.Code, r.Body.String())
				}
			}
			for _, resource := range []string{"bay_id", "technician_id", "vehicle_id"} {
				if n := e.count(`SELECT count(*) FROM appointments a JOIN appointments b
					ON a.id < b.id AND a.` + resource + ` = b.` + resource + `
					AND a.start < b."end" AND b.start < a."end"
					WHERE a.status = 'confirmed' AND b.status = 'confirmed'`); n != 0 {
					t.Errorf("%d overlapping pair(s) on %s", n, resource)
				}
			}
		})
	}
}

// holdInOtherSession runs hold in a transaction on its own pool connection, as a concurrent
// booking would, and returns once hold has run. release ends that transaction, rolled back.
func holdInOtherSession(t *testing.T, e *env, hold func(r *repository.Repository, ctx context.Context) error) (release func()) {
	t.Helper()
	r := repository.New(e.db)
	held, done := make(chan error, 1), make(chan error, 1)
	stop := make(chan struct{})
	go func() {
		done <- r.InTx(context.Background(), func(ctx context.Context) error {
			if err := hold(r, ctx); err != nil {
				held <- err
				return err
			}
			held <- nil
			<-stop
			return errRolledBack
		})
	}()
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	return func() {
		close(stop)
		if err := <-done; !errors.Is(err, errRolledBack) {
			t.Errorf("holding session: %v", err)
		}
	}
}

var errRolledBack = errors.New("rolled back on purpose")

// A booking that waits on the dealership lock past lock_timeout gets a 503, never a 500.
// The lock is held by another session, as a booking at the same dealership would hold it.
func TestN39(t *testing.T) {
	e := newPoolEnv(t)
	release := holdInOtherSession(t, e, func(r *repository.Repository, ctx context.Context) error {
		return r.LockDealership(ctx, "DL1")
	})
	rec := e.book("V1", "OIL", "09:00", key(K5))
	release()
	e.status(rec, http.StatusServiceUnavailable)
	expect(t, "Retry-After", rec.Header().Get("Retry-After"), "1")
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 0)
	expect(t, "key rows", e.count("SELECT count(*) FROM idempotency_keys WHERE key = $1", K5), 0)
}

// lock_timeout is set before the key claim, so a key held past it is a 503 too, not an
// unbounded wait.
func TestHeldKeyIs503(t *testing.T) {
	e := newPoolEnv(t)
	release := holdInOtherSession(t, e, func(r *repository.Repository, ctx context.Context) error {
		_, err := r.ClaimKey(ctx, K5, "other", defaultNow)
		return err
	})
	start := time.Now()
	rec := e.book("V1", "OIL", "09:00", key(K5))
	elapsed := time.Since(start)
	release()
	e.status(rec, http.StatusServiceUnavailable)
	expect(t, "Retry-After", rec.Header().Get("Retry-After"), "1")
	if elapsed > 3*time.Second {
		t.Errorf("returned after %v, want about the 1 s lock timeout", elapsed)
	}
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 0)
	expect(t, "key rows", e.count("SELECT count(*) FROM idempotency_keys WHERE key = $1", K5), 0)
}

// A request deadline that passes during a wait, here shorter than lock_timeout, is a 503 with
// reason deadline, not a 500.
func TestDeadlineIs503(t *testing.T) {
	e := newPoolEnv(t)
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logs, nil))
	svc := service.New(repository.New(e.db), e.clock, log)
	e.h = handler.New(svc, log, handler.Config{Timeout: 300 * time.Millisecond, Registry: prometheus.NewRegistry()})
	release := holdInOtherSession(t, e, func(r *repository.Repository, ctx context.Context) error {
		return r.LockDealership(ctx, "DL1")
	})
	rec := e.book("V1", "OIL", "09:00")
	release()
	e.status(rec, http.StatusServiceUnavailable)
	expect(t, "Retry-After", rec.Header().Get("Retry-After"), "1")
	if !strings.Contains(logs.String(), "result=unavailable reason=deadline") {
		t.Errorf("no unavailable decision with reason=deadline in:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("a deadline was logged as an error:\n%s", logs.String())
	}
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
