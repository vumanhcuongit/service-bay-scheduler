// Package spectest runs every example of docs/superpowers/specs/2026-10-20-booking.md as one test,
// through the real handler, service and repository, against Postgres.
package spectest

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/clock"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/handler"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/repository"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/service"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/testdb"
)

const day = "2026-10-21"

var (
	hcm, _     = time.LoadLocation("Asia/Ho_Chi_Minh")
	defaultNow = time.Date(2026, 10, 20, 12, 0, 0, 0, hcm)
)

type env struct {
	t     *testing.T
	h     http.Handler
	clock *clock.Fixed
	db    repository.DB
}

// newEnv runs everything inside one transaction that is rolled back when the test ends.
func newEnv(t *testing.T) *env {
	return newEnvOn(t, testdb.Tx(t), nil)
}

func newEnvOn(t *testing.T, db repository.DB, wrap func(service.Repository) service.Repository) *env {
	t.Helper()
	var repo service.Repository = repository.New(db)
	if wrap != nil {
		repo = wrap(repo)
	}
	clk := clock.NewFixed(defaultNow)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &env{t: t, h: handler.New(service.New(repo, clk, log), log, handler.Config{}), clock: clk, db: db}
}

type request struct {
	dealership string
	key, rid   string
	extra      map[string]any
	raw        *string
}

type opt func(*request)

func extra(m map[string]any) opt { return func(r *request) { r.extra = m } }

// startAt turns "09:00" or "09:00:30" into Day local time; anything longer is used as given.
func startAt(at string) string {
	switch len(at) {
	case 5:
		return day + "T" + at + ":00+07:00"
	case 8:
		return day + "T" + at + "+07:00"
	}
	return at
}

// book is the spec's book(V, S, t): POST /appointments for DL1 unless dl2() is given.
func (e *env) book(vehicle, serviceType, at string, opts ...opt) *httptest.ResponseRecorder {
	e.t.Helper()
	r := request{dealership: "DL1"}
	for _, o := range opts {
		o(&r)
	}
	body := ""
	if r.raw != nil {
		body = *r.raw
	} else {
		fields := map[string]any{
			"dealership_id":   r.dealership,
			"vehicle_id":      vehicle,
			"service_type_id": serviceType,
			"start":           startAt(at),
		}
		for k, v := range r.extra {
			fields[k] = v
		}
		b, err := json.Marshal(fields)
		if err != nil {
			e.t.Fatal(err)
		}
		body = string(b)
	}
	headers := map[string]string{}
	if r.key != "" {
		headers["Idempotency-Key"] = r.key
	}
	if r.rid != "" {
		headers["X-Request-ID"] = r.rid
	}
	return e.do(http.MethodPost, "/appointments", body, headers)
}

func (e *env) get(path string) *httptest.ResponseRecorder {
	return e.do(http.MethodGet, path, "", nil)
}

func (e *env) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// given inserts a confirmed appointment directly, e.g. given("V1", "OIL", "09:00", "B1", "T1").
func (e *env) given(vehicle, serviceType, at, bay, tech string) {
	e.t.Helper()
	_, err := e.db.Exec(context.Background(), `
		INSERT INTO appointments (dealership_id, customer_id, vehicle_id, service_type_id, bay_id, technician_id, start, "end")
		SELECT st.dealership_id, v.customer_id, v.id, st.id, $3, $4, $5::timestamptz,
		       $5::timestamptz + st.duration_minutes * interval '1 minute'
		FROM service_types st, vehicles v WHERE st.id = $2 AND v.id = $1`,
		vehicle, serviceType, bay, tech, startAt(at))
	if err != nil {
		e.t.Fatalf("given %s %s %s %s/%s: %v", vehicle, serviceType, at, bay, tech, err)
	}
}

type appointment struct {
	ID            string `json:"id"`
	DealershipID  string `json:"dealership_id"`
	CustomerID    string `json:"customer_id"`
	VehicleID     string `json:"vehicle_id"`
	ServiceTypeID string `json:"service_type_id"`
	BayID         string `json:"bay_id"`
	TechnicianID  string `json:"technician_id"`
	Start         string `json:"start"`
	End           string `json:"end"`
	Status        string `json:"status"`
}

// created asserts a 201 on the given bay and technician and returns the body.
func (e *env) created(r *httptest.ResponseRecorder, bay, tech string) appointment {
	e.t.Helper()
	e.status(r, http.StatusCreated)
	var a appointment
	if err := json.Unmarshal(r.Body.Bytes(), &a); err != nil {
		e.t.Fatalf("decode 201 body %q: %v", r.Body.String(), err)
	}
	if a.BayID != bay || a.TechnicianID != tech {
		e.t.Fatalf("assigned %s/%s, want %s/%s", a.BayID, a.TechnicianID, bay, tech)
	}
	return a
}

func (e *env) status(r *httptest.ResponseRecorder, code int) {
	e.t.Helper()
	if r.Code != code {
		e.t.Fatalf("status %d, want %d; body %s", r.Code, code, r.Body.String())
	}
}

func expect[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
