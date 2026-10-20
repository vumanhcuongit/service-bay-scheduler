package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/clock"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/handler"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/repository"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/service"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/testdb"
)

type obsEnv struct {
	h    http.Handler
	reg  *prometheus.Registry
	logs *bytes.Buffer
}

// newObsEnv wires the real stack in a rolled-back transaction, logging JSON into a buffer
// through the context handler, as main does.
func newObsEnv(t *testing.T) obsEnv {
	t.Helper()
	return newObsEnvWith(t, nil)
}

// newObsEnvWith is newObsEnv with InsertAppointment failing with insertErrs, in order, before
// it reaches Postgres.
func newObsEnvWith(t *testing.T, insertErrs []error) obsEnv {
	t.Helper()
	logs := &bytes.Buffer{}
	log := slog.New(handler.NewContextHandler(slog.NewJSONHandler(logs, nil)))
	reg := prometheus.NewRegistry()
	now := time.Date(2026, 10, 20, 5, 0, 0, 0, time.UTC)
	var repo service.Repository = repository.New(testdb.Tx(t))
	if insertErrs != nil {
		repo = &failingInserts{Repository: repo, errs: insertErrs}
	}
	svc := service.New(repo, clock.NewFixed(now), log)
	return obsEnv{h: handler.New(svc, log, handler.Config{Registry: reg}), reg: reg, logs: logs}
}

func (e obsEnv) post(body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/appointments", strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e obsEnv) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

type failingInserts struct {
	service.Repository
	errs []error
}

func (f *failingInserts) InsertAppointment(ctx context.Context, a model.Appointment) (model.Appointment, error) {
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return model.Appointment{}, err
	}
	return f.Repository.InsertAppointment(ctx, a)
}

const oilAt9 = `{"dealership_id":"DL1","vehicle_id":"V1","service_type_id":"OIL","start":"2026-10-21T09:00:00+07:00"}`

func TestRequestIDGeneratedWhenAbsent(t *testing.T) {
	e := newObsEnv(t)
	rec := e.post(oilAt9, nil)
	if id := rec.Header().Get("X-Request-ID"); !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(id) {
		t.Fatalf("X-Request-ID %q, want 32 hex chars", id)
	}
}

func TestBookingMetrics(t *testing.T) {
	e := newObsEnv(t)
	k := map[string]string{"Idempotency-Key": "8a1f2c3d-0000-4000-8000-000000000001"}
	for _, c := range []struct {
		body    string
		headers map[string]string
		code    int
	}{
		{oilAt9, k, http.StatusCreated},                                                           // created
		{oilAt9, k, http.StatusCreated},                                                           // replayed
		{oilAt9, nil, http.StatusConflict},                                                        // conflict: vehicle_busy
		{`{`, nil, http.StatusBadRequest},                                                         // invalid
		{strings.Replace(oilAt9, "DL1", "DL9", 1), nil, http.StatusNotFound},                      // invalid
		{strings.Replace(oilAt9, "09:00:00", "09:05:00", 1), nil, http.StatusUnprocessableEntity}, // invalid
	} {
		if rec := e.post(c.body, c.headers); rec.Code != c.code {
			t.Fatalf("status %d, want %d; body %s", rec.Code, c.code, rec.Body.String())
		}
	}
	e.get("/healthz") // not a booking: counted nowhere

	want := map[string]float64{"created": 1, "replayed": 1, "conflict": 1, "invalid": 3, "unavailable": 0, "error": 0}
	for _, m := range gather(t, e.reg, "booking_attempts_total") {
		result := m["result"].(string)
		if w, ok := want[result]; !ok {
			t.Errorf("unexpected result label %q", result)
		} else if m["value"].(float64) != w {
			t.Errorf("booking_attempts_total{result=%q} = %v, want %v", result, m["value"], want[result])
		}
		delete(want, result)
	}
	if len(want) != 0 {
		t.Errorf("results never exported: %v", want)
	}
	if n, err := testutil.GatherAndCount(e.reg, "booking_latency_seconds"); err != nil || n != 1 {
		t.Fatalf("booking_latency_seconds series = %d, %v", n, err)
	}
	if got := gather(t, e.reg, "booking_latency_seconds")[0]["count"]; got != float64(6) {
		t.Errorf("booking_latency_seconds sample count = %v, want 6", got)
	}
}

// gather returns one map per series of name: its labels, plus "value" for a counter or
// "count" and "sum" for a histogram.
func gather(t *testing.T, reg *prometheus.Registry, name string) []map[string]any {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			s := map[string]any{}
			for _, l := range m.GetLabel() {
				s[l.GetName()] = l.GetValue()
			}
			if c := m.GetCounter(); c != nil {
				s["value"] = c.GetValue()
			}
			if h := m.GetHistogram(); h != nil {
				s["count"] = float64(h.GetSampleCount())
				s["sum"] = h.GetSampleSum()
			}
			out = append(out, s)
		}
	}
	return out
}

func TestLogLinesCarryRequestContext(t *testing.T) {
	e := newObsEnv(t)
	var booked struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(e.post(oilAt9, nil).Body.Bytes(), &booked); err != nil {
		t.Fatal(err)
	}
	e.logs.Reset()

	if rec := e.post(oilAt9, map[string]string{"X-Request-ID": "R9"}); rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", rec.Code)
	}
	var decision map[string]any
	lines := strings.Split(strings.TrimSpace(e.logs.String()), "\n")
	if len(lines) < 3 {
		t.Fatalf("want request in, decision and request out lines, got:\n%s", e.logs.String())
	}
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		if m["request_id"] != "R9" || m["method"] != "POST" || m["path"] != "/appointments" {
			t.Errorf("line lacks request context: %s", l)
		}
		if m["result"] == "conflict" {
			decision = m
		}
	}
	if decision == nil {
		t.Fatalf("no decision line with result conflict:\n%s", e.logs.String())
	}
	if decision["dealership_id"] != "DL1" || decision["reason"] != "vehicle_busy" {
		t.Errorf("decision line %v, want dealership_id DL1, reason vehicle_busy", decision)
	}

	// A read learns the dealership from the appointment it looked up (decisions §5 #16).
	e.logs.Reset()
	if rec := e.get("/appointments/" + booked.ID); rec.Code != http.StatusOK {
		t.Fatalf("GET status %d, want 200", rec.Code)
	}
	var out map[string]any
	for _, l := range strings.Split(strings.TrimSpace(e.logs.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		if m["msg"] == "request out" {
			out = m
		}
	}
	if out["dealership_id"] != "DL1" {
		t.Errorf("GET request out line %v, want dealership_id DL1", out)
	}
}

func TestHealthz(t *testing.T) {
	e := newObsEnv(t)
	rec := e.get("/healthz")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("GET /healthz = %d %s", rec.Code, rec.Body.String())
	}
}

func TestMetricsEndpoint(t *testing.T) {
	e := newObsEnv(t)
	rec := e.get("/metrics")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `booking_attempts_total{result="created"} 0`) {
		t.Fatalf("GET /metrics = %d %s", rec.Code, rec.Body.String())
	}
}

// A 400 never reaches the service, so the handler logs its decision line (design §7).
func TestBadRequestLogsDecision(t *testing.T) {
	e := newObsEnv(t)
	e.post(`{"dealership_id":"DL1"}`, map[string]string{"X-Request-ID": "R4"})
	for _, l := range strings.Split(strings.TrimSpace(e.logs.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		if m["result"] == "invalid" && m["request_id"] == "R4" && m["message"] == "vehicle_id is required" {
			return
		}
	}
	t.Fatalf("no invalid decision line with the 400 message:\n%s", e.logs.String())
}
