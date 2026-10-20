package model

import (
	"cmp"
	"slices"
	"time"
)

// Slot is the booking grid (D1).
const Slot = 15 * time.Minute

// IsAligned reports whether t sits on the 15-minute grid with zero seconds and fractions.
func IsAligned(t time.Time) bool {
	return t.Unix()%int64(Slot/time.Second) == 0 && t.Nanosecond() == 0
}

// Overlaps reports whether two half-open intervals share any instant.
func Overlaps(a, b Interval) bool {
	return a.Start.Before(b.End) && b.Start.Before(a.End)
}

// LocalWindow returns [from, to) on day's local date in loc, as offsets from local midnight.
// Each end is built from wall-clock fields, not midnight plus a duration, which would count
// elapsed time and be an hour out on the day the clocks change.
func LocalWindow(loc *time.Location, day time.Time, from, to time.Duration) (time.Time, time.Time) {
	y, m, d := day.In(loc).Date()
	at := func(offset time.Duration) time.Time {
		return time.Date(y, m, d, int(offset/time.Hour), int(offset%time.Hour/time.Minute), 0, 0, loc)
	}
	return at(from), at(to)
}

// PickResources returns the first free bay and the first free technician by id for [start, end) (D7).
// techs must already be qualified. A technician must also work the whole interval (D4).
// The bay is checked first, so both busy gives ReasonNoBay (D9). reason is "" on success.
// Booking and availability both use this, so they cannot disagree (spec rule 12).
func PickResources(bays []string, techs []Technician, busy []Busy, loc *time.Location, start, end time.Time) (bayID, techID string, reason Reason) {
	want := Interval{start, end}

	bays = slices.Clone(bays)
	slices.Sort(bays)
	for _, b := range bays {
		if !slices.ContainsFunc(busy, func(x Busy) bool { return x.BayID == b && Overlaps(x.Interval, want) }) {
			bayID = b
			break
		}
	}
	if bayID == "" {
		return "", "", ReasonNoBay
	}

	techs = slices.Clone(techs)
	slices.SortFunc(techs, func(a, b Technician) int { return cmp.Compare(a.ID, b.ID) })
	for _, t := range techs {
		from, to := LocalWindow(loc, start, t.WorkStart, t.WorkEnd)
		if start.Before(from) || end.After(to) {
			continue
		}
		if !slices.ContainsFunc(busy, func(x Busy) bool { return x.TechnicianID == t.ID && Overlaps(x.Interval, want) }) {
			return bayID, t.ID, ""
		}
	}
	return "", "", ReasonNoTechnician
}
