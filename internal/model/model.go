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

// StoredKey is a committed idempotency key row.
type StoredKey struct {
	Hash    string
	Outcome Outcome
}

var (
	ErrNotFound        = errors.New("not found")
	ErrUnavailable     = errors.New("lost the race for a bay or technician too many times")
	ErrBayTaken        = errors.New("bay taken")
	ErrTechnicianTaken = errors.New("technician taken")
	ErrVehicleBusy     = errors.New("vehicle busy")
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
