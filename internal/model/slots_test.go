package model

import (
	"testing"
	"time"
)

var hcm = mustLoad("Asia/Ho_Chi_Minh")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// at returns Day (2026-10-21) at hh:mm local time.
func at(hh, mm int) time.Time { return time.Date(2026, 10, 21, hh, mm, 0, 0, hcm) }

func TestIsAligned(t *testing.T) {
	cases := []struct {
		t    time.Time
		want bool
	}{
		{at(9, 0), true},
		{at(9, 10), false},
		{at(9, 0).Add(30 * time.Second), false},
		{at(9, 0).Add(time.Nanosecond), false},
		{time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC), true},
	}
	for _, c := range cases {
		if got := IsAligned(c.t); got != c.want {
			t.Errorf("IsAligned(%s) = %v, want %v", c.t, got, c.want)
		}
	}
}

func TestOverlaps(t *testing.T) {
	a := Interval{at(9, 0), at(9, 30)}
	cases := []struct {
		b    Interval
		want bool
	}{
		{Interval{at(9, 30), at(10, 0)}, false},
		{Interval{at(9, 15), at(9, 45)}, true},
		{Interval{at(8, 30), at(9, 0)}, false},
	}
	for _, c := range cases {
		if got := Overlaps(a, c.b); got != c.want {
			t.Errorf("Overlaps(%v, %v) = %v, want %v", a, c.b, got, c.want)
		}
	}
}

func TestPickResourcesCandidateOrder(t *testing.T) {
	day := 8 * time.Hour
	t1 := Technician{ID: "T1", WorkStart: day, WorkEnd: 17 * time.Hour}
	t2 := Technician{ID: "T2", WorkStart: day, WorkEnd: 13 * time.Hour}
	t3 := Technician{ID: "T3", WorkStart: day, WorkEnd: 17 * time.Hour}
	busy := func(bay, tech string) Busy {
		return Busy{BayID: bay, TechnicianID: tech, Interval: Interval{at(9, 0), at(9, 30)}}
	}

	cases := []struct {
		name       string
		techs      []Technician
		busy       []Busy
		start, end time.Time
		bay, tech  string
		reason     Reason
	}{
		{"lowest ids first", []Technician{t3, t1}, nil, at(9, 0), at(9, 30), "B1", "T1", ""},
		{"skips busy", []Technician{t3, t1}, []Busy{busy("B1", "T1")}, at(9, 0), at(9, 30), "B2", "T3", ""},
		{"bay checked first", []Technician{t3, t1}, []Busy{busy("B1", "T1"), busy("B2", "T3")}, at(9, 0), at(9, 30), "", "", ReasonNoBay},
		{"outside work window", []Technician{t2}, nil, at(12, 30), at(13, 15), "", "", ReasonNoTechnician},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bay, tech, reason := PickResources([]string{"B2", "B1"}, c.techs, c.busy, hcm, c.start, c.end)
			if bay != c.bay || tech != c.tech || reason != c.reason {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)", bay, tech, reason, c.bay, c.tech, c.reason)
			}
		})
	}
}

func TestLocalWindow(t *testing.T) {
	from, to := LocalWindow(hcm, time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC), 8*time.Hour, 17*time.Hour)
	if !from.Equal(at(8, 0)) || !to.Equal(at(17, 0)) {
		t.Errorf("got [%s, %s), want [08:00, 17:00) local", from, to)
	}
}

// Opening hours are wall-clock times, so they hold on the days the clock jumps. Midnight plus
// 8 h counts elapsed time and opens at 09:00 after the spring jump and 07:00 after the autumn one.
func TestLocalWindowOnDSTDays(t *testing.T) {
	ny := mustLoad("America/New_York")
	for _, c := range []struct {
		name              string
		day               time.Time
		openUTC, closeUTC time.Time
	}{
		{"spring forward", time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC), time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC), time.Date(2026, 3, 8, 21, 0, 0, 0, time.UTC)},
		{"fall back", time.Date(2026, 11, 1, 15, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 13, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 22, 0, 0, 0, time.UTC)},
	} {
		t.Run(c.name, func(t *testing.T) {
			from, to := LocalWindow(ny, c.day, 8*time.Hour, 17*time.Hour)
			if !from.Equal(c.openUTC) || !to.Equal(c.closeUTC) {
				t.Errorf("got [%s, %s), want [08:00, 17:00) New York = [%s, %s)", from.In(ny), to.In(ny), c.openUTC, c.closeUTC)
			}
			if from.In(ny).Format("15:04") != "08:00" || to.In(ny).Format("15:04") != "17:00" {
				t.Errorf("got %s to %s local, want 08:00 to 17:00", from.In(ny).Format("15:04"), to.In(ny).Format("15:04"))
			}
		})
	}
}
