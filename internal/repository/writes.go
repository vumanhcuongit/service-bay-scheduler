package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

// exclusionViolation is SQLSTATE exclusion_violation.
const exclusionViolation = "23P01"

// constraintErrors maps each exclusion constraint on appointments to the error the service sees (D8).
var constraintErrors = map[string]error{
	"appointments_bay_excl":        model.ErrBayTaken,
	"appointments_technician_excl": model.ErrTechnicianTaken,
	"appointments_vehicle_excl":    model.ErrVehicleBusy,
}

func (r *Repository) InsertAppointment(ctx context.Context, a model.Appointment) (model.Appointment, error) {
	err := r.q(ctx).QueryRow(ctx, `
		INSERT INTO appointments
			(dealership_id, customer_id, vehicle_id, service_type_id, bay_id, technician_id, start, "end", status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text`,
		a.DealershipID, a.CustomerID, a.VehicleID, a.ServiceTypeID, a.BayID, a.TechnicianID, a.Start, a.End, a.Status).
		Scan(&a.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == exclusionViolation {
			if mapped, ok := constraintErrors[pgErr.ConstraintName]; ok {
				return model.Appointment{}, mapped
			}
		}
		return model.Appointment{}, fmt.Errorf("insert appointment: %w", err)
	}
	return a, nil
}

// ClaimKey is implemented in Task 6; Book does not call it yet.
func (r *Repository) ClaimKey(ctx context.Context, key, hash string, now time.Time) (*model.StoredKey, error) {
	return nil, errors.New("claim key: not implemented")
}

// SaveKeyOutcome is implemented in Task 6; Book does not call it yet.
func (r *Repository) SaveKeyOutcome(ctx context.Context, key string, o model.Outcome) error {
	return errors.New("save key outcome: not implemented")
}
