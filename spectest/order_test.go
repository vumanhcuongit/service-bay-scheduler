package spectest

import (
	"net/http"
	"strings"
	"testing"
)

// Spec rule 7 (400 → key → 404 → 422 → 409), rule 8 (404s name the first unknown) and the
// 422 order in D9. Each input breaks two checks, so only the right order gives the asserted result.

func TestOrder404VehicleBeforeServiceType(t *testing.T) {
	e := newEnv(t)
	r := e.book("V9", "X9", "09:00")
	e.status(r, http.StatusNotFound)
	contains(t, e.message(r), "vehicle")
}

func TestOrder404Before422(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "09:10", dealership("DL9")), http.StatusNotFound)
}

func TestOrder422Before409(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.status(e.book("V1", "OIL", "09:10"), http.StatusUnprocessableEntity)
}

func TestOrder422(t *testing.T) {
	for _, c := range []struct {
		name, serviceType, at, word string
	}{
		{"aligned before future", "OIL", "2026-10-20T09:10:00+07:00", "grid"},
		{"future before ownership", "TIRE", "2026-10-20T09:00:00+07:00", "future"},
		{"ownership before hours", "TIRE", "07:45", "offered"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			r := e.book("V1", c.serviceType, c.at)
			e.status(r, http.StatusUnprocessableEntity)
			contains(t, e.message(r), c.word)
		})
	}
}

func contains(t *testing.T, msg, word string) {
	t.Helper()
	if !strings.Contains(msg, word) {
		t.Errorf("message %q does not contain %q", msg, word)
	}
}

// GET /availability checks 400 → 404 dealership → 404 service type → 422; each case breaks
// two rules and the message names the one that wins.
func TestOrderAvailability(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		name, query string
		code        int
		word        string
	}{
		{"400 before 404", "dealership_id=DL9&service_type_id=OIL", http.StatusBadRequest, "date"},
		{"bad date before 404", "dealership_id=DL9&service_type_id=NOPE&date=2026-13-01", http.StatusBadRequest, "date"},
		{"dealership before service type", "dealership_id=DL9&service_type_id=NOPE&date=" + day, http.StatusNotFound, "dealership DL9"},
		{"404 dealership before 422", "dealership_id=DL9&service_type_id=TIRE&date=" + day, http.StatusNotFound, "dealership DL9"},
		{"404 service type before 422", "dealership_id=DL2&service_type_id=NOPE&date=" + day, http.StatusNotFound, "service type NOPE"},
	} {
		r := e.get("/availability?" + c.query)
		if r.Code != c.code || !strings.Contains(e.message(r), c.word) {
			t.Errorf("%s: status %d, want %d naming %q; body %s", c.name, r.Code, c.code, c.word, r.Body.String())
		}
	}
}
