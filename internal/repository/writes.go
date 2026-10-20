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

// SQLSTATEs mapped to model errors.
const (
	exclusionViolation = "23P01"
	// deadlockDetected: two inserts whose exclusion checks each wait on the other's uncommitted
	// row. The dealership and vehicle locks leave no such pair, so this is a defence, not a path.
	deadlockDetected = "40P01"
	// lockNotAvailable: a lock wait ran past lockTimeout (repository.go).
	lockNotAvailable = "55P03"
)

// constraintErrors maps each exclusion constraint on appointments to the error the service sees (D8).
var constraintErrors = map[string]error{
	"appointments_bay_excl":        model.ErrBayTaken,
	"appointments_technician_excl": model.ErrTechnicianTaken,
	"appointments_vehicle_excl":    model.ErrVehicleBusy,
}

// LockDealership takes the dealership's advisory lock until the transaction ends, so its
// availability reads and insert run one booking at a time and each sees the rows committed
// before it. Without it, concurrent inserts on one slot each wait on the other's uncommitted
// row and deadlock, in chains when there are many; Postgres breaks one cycle per second (D8).
// A wait past lock_timeout (set by InTx) is model.ErrLockTimeout. Outside a transaction the
// lock would be released at once, so that is an error.
func (r *Repository) LockDealership(ctx context.Context, dealershipID string) error {
	return r.advisoryLock(ctx, "dealership", dealershipID)
}

// LockVehicle takes the vehicle's advisory lock until the transaction ends, after the
// dealership lock. One vehicle booked at two dealerships holds a different dealership lock in
// each, so only this lock serialises them; without it their inserts wait on each other's
// uncommitted vehicle row and deadlock (D8). Errors as LockDealership does.
func (r *Repository) LockVehicle(ctx context.Context, vehicleID string) error {
	return r.advisoryLock(ctx, "vehicle", vehicleID)
}

// advisoryLock locks the name kind/id; the prefix keeps a dealership and a vehicle with the same id apart.
func (r *Repository) advisoryLock(ctx context.Context, kind, id string) error {
	tx, err := txFrom(ctx, "lock "+kind)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, kind+"/"+id); err != nil {
		if isCode(err, lockNotAvailable) {
			return model.ErrLockTimeout
		}
		return fmt.Errorf("lock %s: %w", kind, err)
	}
	return nil
}

// txFrom returns the transaction InTx put in ctx. Every write needs it: a context from anywhere
// else would run on the pool outside the transaction, and a lock taken there is released at
// once. Tests nest every InTx in one rolled-back transaction, so the guard cannot rely on
// q(ctx) falling back to the pool.
func txFrom(ctx context.Context, op string) (pgx.Tx, error) {
	tx, ok := ctx.Value(txKey{}).(pgx.Tx)
	if !ok {
		return nil, fmt.Errorf("%s: no transaction in context", op)
	}
	return tx, nil
}

func isCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

// InsertAppointment maps an exclusion violation or deadlock to the error the service sees.
// The constraints stay the guard; the dealership and vehicle locks only keep them from racing (D8).
func (r *Repository) InsertAppointment(ctx context.Context, a model.Appointment) (model.Appointment, error) {
	tx, err := txFrom(ctx, "insert appointment")
	if err != nil {
		return model.Appointment{}, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO appointments
			(dealership_id, customer_id, vehicle_id, service_type_id, bay_id, technician_id, start, "end", status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text`,
		a.DealershipID, a.CustomerID, a.VehicleID, a.ServiceTypeID, a.BayID, a.TechnicianID, a.Start, a.End, a.Status).
		Scan(&a.ID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case exclusionViolation:
				if mapped, ok := constraintErrors[pgErr.ConstraintName]; ok {
					return model.Appointment{}, mapped
				}
			case deadlockDetected:
				return model.Appointment{}, model.ErrDeadlock
			case lockNotAvailable: // waited on another booking's row past lock_timeout
				return model.Appointment{}, model.ErrLockTimeout
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
	tx, err := txFrom(ctx, "claim key")
	if err != nil {
		return nil, err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO idempotency_keys (key, request_hash, created_at) VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE
			SET request_hash = EXCLUDED.request_hash, status = NULL, body = NULL, created_at = EXCLUDED.created_at
			WHERE idempotency_keys.created_at < $3 - interval '24 hours'
		RETURNING key`, key, hash, now).Scan(new(string))
	if err == nil {
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		if isCode(err, lockNotAvailable) { // the holder kept the key past lock_timeout
			return nil, model.ErrLockTimeout
		}
		return nil, fmt.Errorf("claim key: %w", err)
	}
	var stored model.StoredKey
	var body []byte
	if err := tx.QueryRow(ctx, `SELECT request_hash, body FROM idempotency_keys WHERE key = $1`, key).
		Scan(&stored.Hash, &body); err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	if err := json.Unmarshal(body, &stored.Outcome); err != nil {
		return nil, fmt.Errorf("decode key outcome: %w", err)
	}
	return &stored, nil
}

func (r *Repository) SaveKeyOutcome(ctx context.Context, key string, o model.Outcome) error {
	tx, err := txFrom(ctx, "save key outcome")
	if err != nil {
		return err
	}
	status := "created"
	if o.Conflict != nil {
		status = "conflict"
	}
	body, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("encode key outcome: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE idempotency_keys SET status = $2, body = $3 WHERE key = $1`, key, status, body); err != nil {
		return fmt.Errorf("save key outcome: %w", err)
	}
	return nil
}
