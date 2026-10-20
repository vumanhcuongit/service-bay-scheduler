package spectest

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

type availability struct {
	DealershipID  string    `json:"dealership_id"`
	ServiceTypeID string    `json:"service_type_id"`
	Date          string    `json:"date"`
	StartTimes    *[]string `json:"start_times"`
}

// avail is the spec's avail(S, date) for DL1. On a 200 it returns the starts as local "15:04",
// after checking each is UTC RFC 3339.
func (e *env) avail(serviceType, date string) (int, []string) {
	e.t.Helper()
	r := e.get("/availability?dealership_id=DL1&service_type_id=" + serviceType + "&date=" + date)
	if r.Code != http.StatusOK {
		return r.Code, nil
	}
	var a availability
	if err := json.Unmarshal(r.Body.Bytes(), &a); err != nil {
		e.t.Fatalf("decode availability %q: %v", r.Body.String(), err)
	}
	if a.DealershipID != "DL1" || a.ServiceTypeID != serviceType || a.Date != date {
		e.t.Fatalf("echoed %s/%s/%s, want DL1/%s/%s", a.DealershipID, a.ServiceTypeID, a.Date, serviceType, date)
	}
	if a.StartTimes == nil {
		e.t.Fatalf("start_times missing or null: %s", r.Body.String())
	}
	starts := []string{}
	for _, s := range *a.StartTimes {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil || !strings.HasSuffix(s, "Z") {
			e.t.Fatalf("start %q is not UTC RFC 3339", s)
		}
		starts = append(starts, t.In(hcm).Format("15:04"))
	}
	return r.Code, starts
}

// every returns the local starts from first to last inclusive, every 15 minutes.
func every(first, last string) []string {
	from, _ := time.Parse("15:04", first)
	to, _ := time.Parse("15:04", last)
	var out []string
	for t := from; !t.After(to); t = t.Add(15 * time.Minute) {
		out = append(out, t.Format("15:04"))
	}
	return out
}

func TestM21(t *testing.T) {
	e := newEnv(t)
	code, starts := e.avail("OIL", day)
	expect(t, "status", code, http.StatusOK)
	expect(t, "count", len(starts), 35)
	if want := every("08:00", "16:30"); !slices.Equal(starts, want) {
		t.Fatalf("starts %v, want %v", starts, want)
	}
}

func TestM22(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.given("V2", "OIL", "09:00", "B2", "T2")
	code, starts := e.avail("OIL", day)
	expect(t, "status", code, http.StatusOK)
	want := slices.DeleteFunc(every("08:00", "16:30"), func(s string) bool {
		return s == "08:45" || s == "09:00" || s == "09:15"
	})
	if !slices.Equal(starts, want) {
		t.Fatalf("starts %v, want %v", starts, want)
	}
	expect(t, "count", len(starts), 32)
}

func TestM23(t *testing.T) {
	e := newEnv(t)
	code, starts := e.avail("EV_CHECK", day)
	expect(t, "status", code, http.StatusOK)
	if want := every("08:00", "12:15"); !slices.Equal(starts, want) {
		t.Fatalf("starts %v, want %v", starts, want)
	}
}

func TestN34(t *testing.T) {
	e := newEnv(t)
	r := e.get("/availability?dealership_id=DL1&service_type_id=OIL&date=2026-10-19")
	e.status(r, http.StatusOK)
	if !strings.Contains(r.Body.String(), `"start_times":[]`) {
		t.Fatalf("body %s, want start_times []", r.Body.String())
	}
}

// Review Focus 5: bad query parameters never reach the slot loop.
func TestAvailabilityBadParams(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct {
		name, query string
		code        int
		word        string // the message names the failing parameter
	}{
		{"no date", "dealership_id=DL1&service_type_id=OIL", http.StatusBadRequest, "date"},
		{"bad date", "dealership_id=DL1&service_type_id=OIL&date=2026-13-01", http.StatusBadRequest, "date"},
		{"no dealership", "service_type_id=OIL&date=" + day, http.StatusBadRequest, "dealership_id"},
		{"no service type", "dealership_id=DL1&date=" + day, http.StatusBadRequest, "service_type_id"},
		{"unknown dealership", "dealership_id=DL9&service_type_id=OIL&date=" + day, http.StatusNotFound, "dealership DL9"},
		{"unknown service type", "dealership_id=DL1&service_type_id=NOPE&date=" + day, http.StatusNotFound, "service type NOPE"},
		{"repeated dealership", "dealership_id=DL1&dealership_id=DL2&service_type_id=OIL&date=" + day, http.StatusBadRequest, "dealership_id"},
		{"repeated service type", "dealership_id=DL1&service_type_id=OIL&service_type_id=OIL&date=" + day, http.StatusBadRequest, "service_type_id"},
		{"repeated date", "dealership_id=DL1&service_type_id=OIL&date=" + day + "&date=" + day, http.StatusBadRequest, "date"},
		{"repeated empty", "dealership_id=&dealership_id=DL1&service_type_id=OIL&date=" + day, http.StatusBadRequest, "dealership_id"},
		{"service type of another dealership", "dealership_id=DL1&service_type_id=TIRE&date=" + day, http.StatusUnprocessableEntity, "not offered"},
	} {
		r := e.get("/availability?" + c.query)
		if r.Code != c.code || !strings.Contains(e.message(r), c.word) {
			t.Errorf("%s: status %d, want %d naming %q; body %s", c.name, r.Code, c.code, c.word, r.Body.String())
		}
	}
}

// Near now, availability lists exactly what booking accepts (rule 12): at 12:00 the 12:00
// start is a 422, so the first listed start is 12:15.
func TestAvailabilityStartsAfterNow(t *testing.T) {
	e := newEnv(t)
	code, starts := e.avail("OIL", "2026-10-20")
	expect(t, "status", code, http.StatusOK)
	if want := every("12:15", "16:30"); !slices.Equal(starts, want) {
		t.Fatalf("starts %v, want %v", starts, want)
	}
	e.status(e.book("V1", "OIL", "2026-10-20T12:00:00+07:00"), http.StatusUnprocessableEntity)
	e.created(e.book("V1", "OIL", "2026-10-20T12:15:00+07:00"), "B1", "T1")
}

// date is a local date in the dealership's timezone, not UTC. The seed only has +07:00, where
// UTC midnight falls on the same local date, so this adds a dealership at UTC−4 on Day.
func TestAvailabilityDateIsDealershipLocal(t *testing.T) {
	e := newEnv(t)
	if _, err := e.db.Exec(context.Background(), `
		INSERT INTO dealerships (id, timezone, open_time, close_time) VALUES ('DLNY', 'America/New_York', '08:00', '17:00');
		INSERT INTO bays (id, dealership_id) VALUES ('BNY', 'DLNY');
		INSERT INTO technicians (id, dealership_id, work_start, work_end) VALUES ('TNY', 'DLNY', '08:00', '17:00');
		INSERT INTO technician_skills (technician_id, skill_id) VALUES ('TNY', 'oil');
		INSERT INTO service_types (id, dealership_id, duration_minutes, required_skill) VALUES ('OIL_NY', 'DLNY', 30, 'oil');`); err != nil {
		t.Fatal(err)
	}
	r := e.get("/availability?dealership_id=DLNY&service_type_id=OIL_NY&date=" + day)
	e.status(r, http.StatusOK)
	var a availability
	if err := json.Unmarshal(r.Body.Bytes(), &a); err != nil || a.StartTimes == nil {
		t.Fatalf("decode %s: %v", r.Body.String(), err)
	}
	starts := *a.StartTimes
	// 08:00 and 16:30 EDT on 2026-10-21.
	if len(starts) != 35 || starts[0] != "2026-10-21T12:00:00Z" || starts[34] != "2026-10-21T20:30:00Z" {
		t.Fatalf("starts %v, want 35 from 2026-10-21T12:00:00Z to 2026-10-21T20:30:00Z", starts)
	}
}
