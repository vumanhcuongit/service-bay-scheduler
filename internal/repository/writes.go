package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
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

// ClaimKey inserts the key row. On a conflict with a row older than 24 h at now, it takes
// that row over; with a fresh one it returns nothing, but ON CONFLICT DO UPDATE still waits
// for the holder to commit and then locks the row, so the SELECT reads its committed outcome.
func (r *Repository) ClaimKey(ctx context.Context, key, hash string, now time.Time) (*model.StoredKey, error) {
	q := r.q(ctx)
	err := q.QueryRow(ctx, `
		INSERT INTO idempotency_keys (key, request_hash, created_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE
			SET request_hash = EXCLUDED.request_hash, status = NULL, body = NULL, created_at = EXCLUDED.created_at
			WHERE idempotency_keys.created_at < $3 - interval '24 hours'
		RETURNING key`, key, hash, now).Scan(new(string))
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("claim key: %w", err)
	}
	var stored model.StoredKey
	var body []byte
	if err := q.QueryRow(ctx, `SELECT request_hash, body FROM idempotency_keys WHERE key = $1`, key).
		Scan(&stored.Hash, &body); err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	if err := json.Unmarshal(body, &stored.Outcome); err != nil {
		return nil, fmt.Errorf("decode key outcome: %w", err)
	}
	return &stored, nil
}

func (r *Repository) SaveKeyOutcome(ctx context.Context, key string, o model.Outcome) error {
	status := "created"
	if o.Conflict != nil {
		status = "conflict"
	}
	body, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("encode key outcome: %w", err)
	}
	if _, err := r.q(ctx).Exec(ctx, `UPDATE idempotency_keys SET status = $2, body = $3 WHERE key = $1`, key, status, body); err != nil {
		return fmt.Errorf("save key outcome: %w", err)
	}
	return nil
}
