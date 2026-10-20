// Package service owns the booking rules: check order, assignment and availability.
// It knows nothing about HTTP or SQL.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

type Service struct {
	repo  Repository
	clock model.Clock
	log   *slog.Logger
}

func New(r Repository, c model.Clock, l *slog.Logger) *Service {
	return &Service{repo: r, clock: c, log: l}
}

// maxAttempts bounds the transactions one booking runs after losing races; before each retry
// it waits minBackoff plus up to minBackoff more, at random (D8).
const (
	maxAttempts = 3
	minBackoff  = 50 * time.Millisecond
)

// Book books req, assigning the first free bay and qualified technician (D7).
// key is the Idempotency-Key, "" for none; it is claimed before any other check, so a stored
// outcome replays without them (D9). A 409 is an Outcome, not an error. A lost race for a bay
// or technician retries with a fresh check, at most maxAttempts in total, then ErrUnavailable (D8).
func (s *Service) Book(ctx context.Context, req model.BookingRequest, key string) (model.Outcome, error) {
	vehicleRerun := false
	for attempt := 1; ; {
		out, err := s.bookOnce(ctx, req, key)
		switch {
		case errors.Is(err, model.ErrVehicleBusy):
			// The other booking has committed, so a fresh check normally returns the 409 and
			// stores it on the key. It is not a lost race, so it does not count (D8).
			if vehicleRerun {
				return conflict(model.ReasonVehicleBusy, fmt.Sprintf("vehicle %s already has an overlapping appointment", req.VehicleID)), nil
			}
			vehicleRerun = true
		case errors.Is(err, model.ErrBayTaken), errors.Is(err, model.ErrTechnicianTaken):
			if attempt == maxAttempts {
				return model.Outcome{}, model.ErrUnavailable
			}
			s.log.InfoContext(ctx, "lost a race for a bay or technician", "result", "retry", "attempt", attempt)
			if err := sleep(ctx, minBackoff+rand.N(minBackoff)); err != nil {
				return model.Outcome{}, err
			}
			attempt++
		default:
			return out, err
		}
	}
}

// bookOnce runs one booking transaction: claim the key, then attempt, then store the outcome.
func (s *Service) bookOnce(ctx context.Context, req model.BookingRequest, key string) (model.Outcome, error) {
	var out model.Outcome
	err := s.repo.InTx(ctx, func(ctx context.Context) error {
		if key != "" {
			hash := model.HashRequest(req)
			stored, err := s.repo.ClaimKey(ctx, key, hash, s.clock.Now())
			if err != nil {
				return err
			}
			if stored != nil {
				if stored.Hash != hash {
					return invalid("Idempotency-Key reused with a different payload")
				}
				out = stored.Outcome
				out.Replayed = true
				return nil
			}
		}
		var err error
		if out, err = s.attempt(ctx, req); err != nil {
			return err
		}
		if key != "" {
			// Same transaction as the appointment, so the key row and the booking commit together.
			return s.repo.SaveKeyOutcome(ctx, key, out)
		}
		return nil
	})
	return out, err
}

// attempt runs one booking transaction body: 404 → 422 → 409 → insert (D9).
func (s *Service) attempt(ctx context.Context, req model.BookingRequest) (model.Outcome, error) {
	d, err := s.repo.GetDealership(ctx, req.DealershipID)
	if err != nil {
		return model.Outcome{}, notFound(err, "dealership", req.DealershipID)
	}
	v, err := s.repo.GetVehicle(ctx, req.VehicleID)
	if err != nil {
		return model.Outcome{}, notFound(err, "vehicle", req.VehicleID)
	}
	st, err := s.repo.GetServiceType(ctx, req.ServiceTypeID)
	if err != nil {
		return model.Outcome{}, notFound(err, "service type", req.ServiceTypeID)
	}
	if err := checkRules(d, st, req.Start, s.clock.Now()); err != nil {
		return model.Outcome{}, err
	}

	w := model.Interval{Start: req.Start, End: req.Start.Add(st.Duration)}
	span := fmt.Sprintf("%s–%s", w.Start.In(d.Location).Format("15:04"), w.End.In(d.Location).Format("15:04"))

	vehicleHasOverlap, err := s.repo.VehicleHasOverlap(ctx, v.ID, w)
	if err != nil {
		return model.Outcome{}, err
	}
	if vehicleHasOverlap {
		return conflict(model.ReasonVehicleBusy, fmt.Sprintf("vehicle %s already has an appointment overlapping %s", v.ID, span)), nil
	}

	bays, err := s.repo.ListBays(ctx, d.ID)
	if err != nil {
		return model.Outcome{}, err
	}
	techs, err := s.repo.ListQualifiedTechnicians(ctx, d.ID, st.RequiredSkill)
	if err != nil {
		return model.Outcome{}, err
	}
	taken, err := s.repo.ListBusy(ctx, d.ID, w)
	if err != nil {
		return model.Outcome{}, err
	}
	bay, tech, reason := model.PickResources(bays, techs, taken, d.Location, w.Start, w.End)
	switch reason {
	case model.ReasonNoBay:
		return conflict(reason, "no bay free for "+span), nil
	case model.ReasonNoTechnician:
		return conflict(reason, "no qualified technician free for "+span), nil
	}

	a, err := s.repo.InsertAppointment(ctx, model.Appointment{
		DealershipID:  d.ID,
		CustomerID:    v.CustomerID,
		VehicleID:     v.ID,
		ServiceTypeID: st.ID,
		BayID:         bay,
		TechnicianID:  tech,
		Start:         w.Start,
		End:           w.End,
		Status:        model.StatusConfirmed,
	})
	if err != nil {
		return model.Outcome{}, err
	}
	return model.Outcome{Appointment: &a}, nil
}

// sleep waits for d, or returns ctx's error as soon as it is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// GetAppointment returns a booked appointment, or *model.NotFoundError.
func (s *Service) GetAppointment(ctx context.Context, id string) (model.Appointment, error) {
	a, err := s.repo.GetAppointment(ctx, id)
	if err != nil {
		return model.Appointment{}, notFound(err, "appointment", id)
	}
	return a, nil
}

// checkRules runs the 422 checks in the D9 order: grid, future, service type offered here, opening hours.
func checkRules(d model.Dealership, st model.ServiceType, start, now time.Time) error {
	local := start.In(d.Location)
	switch {
	case !model.IsAligned(start):
		return invalid("start %s is not on the 15-minute grid", local.Format(time.RFC3339Nano))
	case !start.After(now):
		return invalid("start %s is not in the future", local.Format(time.RFC3339))
	case st.DealershipID != d.ID:
		return invalid("service type %s is not offered by dealership %s", st.ID, d.ID)
	}
	opening, closing := model.LocalWindow(d.Location, start, d.Open, d.Close)
	end := start.Add(st.Duration)
	switch {
	case start.Before(opening):
		return invalid("appointment would start at %s, before opening at %s", local.Format("15:04"), opening.Format("15:04"))
	case end.After(closing):
		return invalid("appointment would end at %s, after closing at %s", end.In(d.Location).Format("15:04"), closing.Format("15:04"))
	}
	return nil
}

func invalid(format string, args ...any) error {
	return &model.InvalidError{Message: fmt.Sprintf(format, args...)}
}

func conflict(r model.Reason, msg string) model.Outcome {
	return model.Outcome{Conflict: &model.Conflict{Reason: r, Message: msg}}
}

// notFound turns the repository's ErrNotFound into a NotFoundError naming the reference.
func notFound(err error, what, id string) error {
	if errors.Is(err, model.ErrNotFound) {
		return &model.NotFoundError{What: what, ID: id}
	}
	return err
}
