package service

import (
	"context"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

// Repository is the storage the service needs. The verb prefix states the contract:
// Get returns one row or model.ErrNotFound, List returns many (empty is not an error),
// Has returns a bool, and Claim/Save/Lock/Insert name the write.
type Repository interface {
	// InTx runs fn in a transaction and commits iff fn returns nil. Repository calls made
	// with fn's ctx run in that transaction; calls with any other ctx run outside it.
	InTx(ctx context.Context, fn func(ctx context.Context) error) error

	GetDealership(ctx context.Context, id string) (model.Dealership, error)
	GetVehicle(ctx context.Context, id string) (model.Vehicle, error)
	GetServiceType(ctx context.Context, id string) (model.ServiceType, error)
	GetAppointment(ctx context.Context, id string) (model.Appointment, error)

	ListBays(ctx context.Context, dealershipID string) ([]string, error)
	ListQualifiedTechnicians(ctx context.Context, dealershipID, skill string) ([]model.Technician, error)
	// ListBusy returns the dealership's confirmed appointments that overlap w.
	ListBusy(ctx context.Context, dealershipID string, w model.Interval) ([]model.Busy, error)
	// VehicleHasOverlap reports a confirmed appointment for the vehicle overlapping w, at any dealership.
	VehicleHasOverlap(ctx context.Context, vehicleID string, w model.Interval) (bool, error)

	// ClaimKey inserts the key row first; it waits on a concurrent holder of the same key.
	// It returns nil when the key is claimed, or the committed row when one is fresh at now.
	ClaimKey(ctx context.Context, key, hash string, now time.Time) (*model.StoredKey, error)
	SaveKeyOutcome(ctx context.Context, key string, o model.Outcome) error
	// LockDealership serialises the dealership's availability reads and inserts until the
	// transaction ends. ctx must carry a transaction (InTx); without one it returns an error.
	// A wait past the lock timeout is model.ErrLockTimeout.
	LockDealership(ctx context.Context, dealershipID string) error
	// InsertAppointment returns the appointment with its ID, or model.ErrBayTaken,
	// model.ErrTechnicianTaken or model.ErrVehicleBusy when an exclusion constraint rejects it,
	// or model.ErrDeadlock when Postgres aborts it to break a deadlock with another insert,
	// or model.ErrLockTimeout when it waited on another booking's row past the lock timeout.
	InsertAppointment(ctx context.Context, a model.Appointment) (model.Appointment, error)
}
