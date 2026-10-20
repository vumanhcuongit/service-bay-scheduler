// Package model holds the booking types, errors and pure rules shared by every layer.
// It imports nothing from the other internal packages.
package model

import (
	"errors"
	"fmt"
	"time"
)

// Dealership hours are offsets from local midnight in Location (D2).
type Dealership struct {
	ID          string
	Location    *time.Location
	Open, Close time.Duration
}

// Technician working hours are offsets from local midnight in the dealership's location (D4).
type Technician struct {
	ID                 string
	WorkStart, WorkEnd time.Duration
}

type ServiceType struct {
	ID, DealershipID, RequiredSkill string
	Duration                        time.Duration
}

type Vehicle struct {
	ID, CustomerID string
}

// Interval is half-open: [Start, End) (D6).
type Interval struct {
	Start, End time.Time
}

// Busy is a confirmed appointment as seen by assignment: which bay and technician it holds, and when.
type Busy struct {
	BayID, TechnicianID string
	Interval
}

type BookingRequest struct {
	DealershipID, VehicleID, ServiceTypeID string
	Start                                  time.Time
}

type Appointment struct {
	ID, DealershipID, CustomerID, VehicleID, ServiceTypeID, BayID, TechnicianID string
	Start, End                                                                  time.Time
	Status                                                                      string
}

const StatusConfirmed = "confirmed"

// Reason is a 409 reason. Checked in this order, first failure wins (D9).
type Reason string

const (
	ReasonVehicleBusy  Reason = "vehicle_busy"
	ReasonNoBay        Reason = "no_bay"
	ReasonNoTechnician Reason = "no_technician"
)

type Conflict struct {
	Reason  Reason
	Message string
}

// Outcome is the result of a booking that is stored on the idempotency key and replayed.
// Exactly one of Appointment and Conflict is set. Replayed is never stored.
type Outcome struct {
	Appointment *Appointment `json:",omitempty"`
	Conflict    *Conflict    `json:",omitempty"`
	Replayed    bool         `json:"-"`
}

// Result names what happened to a booking, in its decision log line and as the
// booking_attempts_total label (D13). 400, 404 and 422 are all ResultInvalid.
type Result string

const (
	ResultCreated     Result = "created"
	ResultReplayed    Result = "replayed"
	ResultConflict    Result = "conflict"
	ResultInvalid     Result = "invalid"
	ResultUnavailable Result = "unavailable"
	ResultError       Result = "error"
	// ResultRetry is logged for the re-run; it is not a final result, so not a label.
	ResultRetry Result = "retry"
)

// Results are the booking_attempts_total labels.
var Results = []Result{ResultCreated, ResultReplayed, ResultConflict, ResultInvalid, ResultUnavailable, ResultError}

// Result is ResultConflict or ResultCreated, ignoring Replayed.
func (o Outcome) Result() Result {
	if o.Conflict != nil {
		return ResultConflict
	}
	return ResultCreated
}

// StoredKey is a committed idempotency key row.
type StoredKey struct {
	Hash    string
	Outcome Outcome
}

var (
	ErrNotFound = errors.New("not found")
	// ErrUnavailable is a booking that could not complete under contention: a constraint
	// violation or deadlock on the re-run too, or a lock wait past the lock timeout (D8).
	ErrUnavailable     = errors.New("booking unavailable under contention")
	ErrBayTaken        = errors.New("bay taken")
	ErrTechnicianTaken = errors.New("technician taken")
	ErrVehicleBusy     = errors.New("vehicle busy")
	// ErrDeadlock is two bookings whose exclusion checks waited on each other; Postgres
	// aborted this one. Like a constraint violation it gets the one re-run (D8).
	ErrDeadlock = errors.New("deadlocked with a concurrent booking")
	// ErrLockTimeout is a wait on the dealership lock, or on another booking's row, past the
	// lock timeout. It is not re-run: the wait already used most of the budget (D8).
	ErrLockTimeout = errors.New("lock wait timed out")
)

// NotFoundError names a well-formed reference that does not exist (404).
type NotFoundError struct {
	What, ID string
}

func (e *NotFoundError) Error() string { return fmt.Sprintf("%s %s not found", e.What, e.ID) }

// InvalidError is a request that is well-formed but breaks a rule (422).
type InvalidError struct {
	Message string
}

func (e *InvalidError) Error() string { return e.Message }
