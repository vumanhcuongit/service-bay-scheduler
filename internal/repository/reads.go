package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

// Times of day are read as seconds since midnight, so they map straight to time.Duration.

func (r *Repository) GetDealership(ctx context.Context, id string) (model.Dealership, error) {
	var tz string
	var open, closing int64
	err := r.q(ctx).QueryRow(ctx, `
		SELECT timezone, EXTRACT(EPOCH FROM open_time)::bigint, EXTRACT(EPOCH FROM close_time)::bigint
		FROM dealerships WHERE id = $1`, id).Scan(&tz, &open, &closing)
	if err != nil {
		return model.Dealership{}, rowErr("dealership", err)
	}
	loc, err := location(tz)
	if err != nil {
		return model.Dealership{}, fmt.Errorf("dealership %s timezone: %w", id, err)
	}
	return model.Dealership{ID: id, Location: loc, Open: seconds(open), Close: seconds(closing)}, nil
}

func (r *Repository) GetVehicle(ctx context.Context, id string) (model.Vehicle, error) {
	v := model.Vehicle{ID: id}
	err := r.q(ctx).QueryRow(ctx, `SELECT customer_id FROM vehicles WHERE id = $1`, id).Scan(&v.CustomerID)
	if err != nil {
		return model.Vehicle{}, rowErr("vehicle", err)
	}
	return v, nil
}

func (r *Repository) GetServiceType(ctx context.Context, id string) (model.ServiceType, error) {
	st := model.ServiceType{ID: id}
	var minutes int64
	err := r.q(ctx).QueryRow(ctx, `
		SELECT dealership_id, duration_minutes, required_skill FROM service_types WHERE id = $1`, id).
		Scan(&st.DealershipID, &minutes, &st.RequiredSkill)
	if err != nil {
		return model.ServiceType{}, rowErr("service type", err)
	}
	st.Duration = time.Duration(minutes) * time.Minute
	return st, nil
}

// GetAppointment expects id to be a UUID; the handler checks that before calling.
func (r *Repository) GetAppointment(ctx context.Context, id string) (model.Appointment, error) {
	var a model.Appointment
	err := r.q(ctx).QueryRow(ctx, `
		SELECT id::text, dealership_id, customer_id, vehicle_id, service_type_id, bay_id, technician_id, start, "end", status
		FROM appointments WHERE id = $1`, id).
		Scan(&a.ID, &a.DealershipID, &a.CustomerID, &a.VehicleID, &a.ServiceTypeID, &a.BayID, &a.TechnicianID, &a.Start, &a.End, &a.Status)
	if err != nil {
		return model.Appointment{}, rowErr("appointment", err)
	}
	return a, nil
}

func (r *Repository) ListBays(ctx context.Context, dealershipID string) ([]string, error) {
	rows, err := r.q(ctx).Query(ctx, `SELECT id FROM bays WHERE dealership_id = $1 ORDER BY id`, dealershipID)
	if err != nil {
		return nil, fmt.Errorf("list bays: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (r *Repository) ListQualifiedTechnicians(ctx context.Context, dealershipID, skill string) ([]model.Technician, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT t.id, EXTRACT(EPOCH FROM t.work_start)::bigint, EXTRACT(EPOCH FROM t.work_end)::bigint
		FROM technicians t JOIN technician_skills ts ON ts.technician_id = t.id
		WHERE t.dealership_id = $1 AND ts.skill_id = $2
		ORDER BY t.id`, dealershipID, skill)
	if err != nil {
		return nil, fmt.Errorf("list technicians: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Technician, error) {
		var t model.Technician
		var from, to int64
		err := row.Scan(&t.ID, &from, &to)
		t.WorkStart, t.WorkEnd = seconds(from), seconds(to)
		return t, err
	})
}

func (r *Repository) ListBusy(ctx context.Context, dealershipID string, w model.Interval) ([]model.Busy, error) {
	rows, err := r.q(ctx).Query(ctx, `
		SELECT bay_id, technician_id, start, "end" FROM appointments
		WHERE dealership_id = $1 AND status = 'confirmed'
		  AND tstzrange(start, "end") && tstzrange($2, $3)`, dealershipID, w.Start, w.End)
	if err != nil {
		return nil, fmt.Errorf("list busy: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Busy, error) {
		var b model.Busy
		err := row.Scan(&b.BayID, &b.TechnicianID, &b.Start, &b.End)
		return b, err
	})
}

func (r *Repository) VehicleHasOverlap(ctx context.Context, vehicleID string, w model.Interval) (bool, error) {
	var exists bool
	err := r.q(ctx).QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM appointments
		WHERE vehicle_id = $1 AND status = 'confirmed'
		  AND tstzrange(start, "end") && tstzrange($2, $3))`, vehicleID, w.Start, w.End).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("vehicle overlap: %w", err)
	}
	return exists, nil
}

// locations caches time.LoadLocation by name; a *time.Location is safe to share.
var locations sync.Map

func location(name string) (*time.Location, error) {
	if loc, ok := locations.Load(name); ok {
		return loc.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	locations.Store(name, loc)
	return loc, nil
}

func seconds(s int64) time.Duration { return time.Duration(s) * time.Second }

// rowErr maps a missing row to model.ErrNotFound and wraps anything else.
func rowErr(what string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	return fmt.Errorf("get %s: %w", what, err)
}
