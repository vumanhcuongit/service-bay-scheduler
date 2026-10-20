package service

import (
	"context"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

// ListAvailability returns every aligned start on date, a local date in the dealership's
// location, where booking would assign a bay and a technician now (spec rule 12). The vehicle
// is not considered (D9). Errors are 404 dealership → 404 service type → 422 not offered here.
// The day is read once and checked in memory with the booking's own PickResources (design §8.1).
func (s *Service) ListAvailability(ctx context.Context, dealershipID, serviceTypeID string, date time.Time) ([]time.Time, error) {
	d, err := s.repo.GetDealership(ctx, dealershipID)
	if err != nil {
		return nil, notFound(err, "dealership", dealershipID)
	}
	st, err := s.repo.GetServiceType(ctx, serviceTypeID)
	if err != nil {
		return nil, notFound(err, "service type", serviceTypeID)
	}
	if st.DealershipID != d.ID {
		return nil, invalid("service type %s is not offered by dealership %s", st.ID, d.ID)
	}

	y, m, day := date.Date()
	opening, closing := model.LocalWindow(d.Location, time.Date(y, m, day, 0, 0, 0, 0, d.Location), d.Open, d.Close)

	bays, err := s.repo.ListBays(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	techs, err := s.repo.ListQualifiedTechnicians(ctx, d.ID, st.RequiredSkill)
	if err != nil {
		return nil, err
	}
	busy, err := s.repo.ListBusy(ctx, d.ID, model.Interval{Start: opening, End: closing})
	if err != nil {
		return nil, err
	}
	starts := []time.Time{}
	now := s.clock.Now()
	for start := opening; !start.Add(st.Duration).After(closing); start = start.Add(model.Slot) {
		if !start.After(now) {
			continue
		}
		if _, _, reason := model.PickResources(bays, techs, busy, d.Location, start, start.Add(st.Duration)); reason == "" {
			starts = append(starts, start)
		}
	}
	return starts, nil
}
