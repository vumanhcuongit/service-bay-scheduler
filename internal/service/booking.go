// Package service owns the booking rules: check order, assignment and availability.
// It knows nothing about HTTP or SQL.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

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

// Book books req, assigning the first free bay and qualified technician (D7).
// key is the Idempotency-Key, "" for none. A 409 is an Outcome, not an error.
func (s *Service) Book(ctx context.Context, req model.BookingRequest, key string) (model.Outcome, error) {
	var out model.Outcome
	err := s.repo.InTx(ctx, func(ctx context.Context) error {
		var err error
		out, err = s.attempt(ctx, req)
		return err
	})
	return out, err
}

// attempt runs one booking transaction body: 404 → 409 → insert.
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

// GetAppointment returns a booked appointment, or *model.NotFoundError.
func (s *Service) GetAppointment(ctx context.Context, id string) (model.Appointment, error) {
	a, err := s.repo.GetAppointment(ctx, id)
	if err != nil {
		return model.Appointment{}, notFound(err, "appointment", id)
	}
	return a, nil
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
