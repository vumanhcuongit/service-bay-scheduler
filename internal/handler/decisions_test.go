package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

type logLine map[string]any

func (e obsEnv) lines(t *testing.T) []logLine {
	t.Helper()
	var out []logLine
	for _, l := range strings.Split(strings.TrimSpace(e.logs.String()), "\n") {
		if l == "" {
			continue
		}
		var m logLine
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}

// counts returns booking_attempts_total by result.
func (e obsEnv) counts(t *testing.T) map[string]float64 {
	t.Helper()
	out := map[string]float64{}
	for _, m := range gather(t, e.reg, "booking_attempts_total") {
		out[m["result"].(string)] = m["value"].(float64)
	}
	return out
}

// Every booking logs exactly one decision line with its reason (design §7, D13), and its
// result is the label booking_attempts_total counts, so logs and metrics cannot drift apart.
func TestDecisionLines(t *testing.T) {
	const k = "8a1f2c3d-0000-4000-8000-000000000002"
	type req struct{ body, key string }
	for _, c := range []struct {
		name       string
		insertErrs []error
		setup      []req
		req        req
		result     string
		retries    int
		attrs      logLine
	}{
		{name: "created", req: req{body: oilAt9}, result: "created",
			attrs: logLine{"bay_id": "B1", "technician_id": "T1"}},
		{name: "replayed 201", setup: []req{{oilAt9, k}}, req: req{oilAt9, k}, result: "replayed",
			attrs: logLine{"outcome": "created"}},
		{name: "replayed 409", setup: []req{{oilAt9, ""}, {oilAt9, k}}, req: req{oilAt9, k}, result: "replayed",
			attrs: logLine{"outcome": "conflict", "reason": "vehicle_busy"}},
		{name: "conflict", setup: []req{{oilAt9, ""}}, req: req{body: oilAt9}, result: "conflict",
			attrs: logLine{"reason": "vehicle_busy"}},
		{name: "400", req: req{body: `{`}, result: "invalid",
			attrs: logLine{"status": float64(400), "message": "body must be a JSON object"}},
		{name: "404", req: req{body: strings.Replace(oilAt9, "DL1", "DL9", 1)}, result: "invalid",
			attrs: logLine{"status": float64(404), "message": "dealership DL9 not found"}},
		{name: "422", req: req{body: strings.Replace(oilAt9, "09:00:00", "09:05:00", 1)}, result: "invalid",
			attrs: logLine{"status": float64(422)}},
		{name: "retry then created", insertErrs: []error{model.ErrTechnicianTaken}, req: req{body: oilAt9},
			result: "created", retries: 1},
		{name: "unavailable", insertErrs: []error{model.ErrBayTaken, model.ErrBayTaken, model.ErrBayTaken},
			req: req{body: oilAt9}, result: "unavailable", retries: 2},
		{name: "error", insertErrs: []error{errors.New("boom")}, req: req{body: oilAt9}, result: "error",
			attrs: logLine{"error": "boom"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newObsEnvWith(t, c.insertErrs)
			for _, s := range c.setup {
				e.post(s.body, map[string]string{"Idempotency-Key": s.key})
			}
			before := e.counts(t)
			e.logs.Reset()

			headers := map[string]string{"X-Request-ID": "RX"}
			if c.req.key != "" {
				headers["Idempotency-Key"] = c.req.key
			}
			e.post(c.req.body, headers)

			var decisions []logLine
			retries := 0
			for _, l := range e.lines(t) {
				if l["request_id"] != "RX" {
					t.Errorf("line without request_id RX: %v", l)
				}
				switch l["result"] {
				case nil:
				case "retry":
					retries++
				default:
					decisions = append(decisions, l)
				}
			}
			if len(decisions) != 1 {
				t.Fatalf("decision lines %v, want exactly one", decisions)
			}
			d := decisions[0]
			if d["result"] != c.result || retries != c.retries {
				t.Errorf("result %v with %d retries, want %s with %d", d["result"], retries, c.result, c.retries)
			}
			for key, want := range c.attrs {
				if d[key] != want {
					t.Errorf("decision %s = %v, want %v; line %v", key, d[key], want, d)
				}
			}

			after := e.counts(t)
			for result, n := range after {
				want := before[result]
				if result == c.result {
					want++
				}
				if n != want {
					t.Errorf("booking_attempts_total{result=%q} went %v → %v; logged result %s", result, before[result], n, c.result)
				}
			}
		})
	}
}

// The histogram times the whole request, so a 503 after three attempts includes both backoffs.
func TestLatencyIncludesRetries(t *testing.T) {
	e := newObsEnvWith(t, []error{model.ErrBayTaken, model.ErrBayTaken, model.ErrBayTaken})
	if rec := e.post(oilAt9, nil); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503", rec.Code)
	}
	h := gather(t, e.reg, "booking_latency_seconds")[0]
	if h["count"] != float64(1) || h["sum"].(float64) < 0.1 {
		t.Fatalf("latency count %v sum %v, want 1 sample ≥ 0.1 s (two 50 ms backoffs)", h["count"], h["sum"])
	}
}

// Rejections on the GET routes are explained by their own line too.
func TestGetRejectionsLogged(t *testing.T) {
	for path, want := range map[string]logLine{
		"/availability?dealership_id=DL1&service_type_id=OIL":                  {"status": float64(400), "message": "date is required"},
		"/availability?dealership_id=DL9&service_type_id=OIL&date=2026-10-21":  {"status": float64(404), "message": "dealership DL9 not found", "dealership_id": "DL9"},
		"/availability?dealership_id=DL1&service_type_id=TIRE&date=2026-10-21": {"status": float64(422), "message": "service type TIRE is not offered by dealership DL1"},
		"/appointments/abc": {"status": float64(400), "message": "id must be a UUID"},
	} {
		e := newObsEnv(t)
		e.get(path)
		var found bool
		for _, l := range e.lines(t) {
			if l["msg"] == "request rejected" {
				found = true
				for key, v := range want {
					if l[key] != v {
						t.Errorf("%s: %s = %v, want %v", path, key, l[key], v)
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: no request rejected line:\n%s", path, e.logs.String())
		}
	}
}

// A caller's X-Request-ID goes into every log line, so only a short, plain one is kept.
func TestRequestIDValidated(t *testing.T) {
	generated := regexp.MustCompile(`^[0-9a-f]{32}$`)
	for id, kept := range map[string]bool{
		"R9":                     true,
		"a.b_c-D9":               true,
		strings.Repeat("x", 128): true,
		strings.Repeat("x", 129): false,
		"has space":              false,
		`quote"`:                 false,
	} {
		e := newObsEnv(t)
		rec := httptestGet(e, "/healthz", id)
		got := rec.Header().Get("X-Request-ID")
		if kept && got != id {
			t.Errorf("%q: echoed %q, want it kept", id, got)
		}
		if !kept && !generated.MatchString(got) {
			t.Errorf("%q: echoed %q, want a generated id", id, got)
		}
	}
}

// Health checks and scrapes are polled constantly, so they write no access log lines.
func TestProbesNotAccessLogged(t *testing.T) {
	e := newObsEnv(t)
	e.get("/healthz")
	e.get("/metrics")
	if e.logs.Len() != 0 {
		t.Fatalf("probe logs:\n%s", e.logs.String())
	}
	e.get("/appointments/abc")
	if e.logs.Len() == 0 {
		t.Fatal("other routes must still be access logged")
	}
}

func httptestGet(e obsEnv, path, requestID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Request-ID", requestID)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}
