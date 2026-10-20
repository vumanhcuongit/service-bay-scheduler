package service_test

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/clock"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/service"
)

// recorder is a repository that answers one free bay and technician and records each call.
type recorder struct{ calls []string }

func (r *recorder) record(name string) { r.calls = append(r.calls, name) }

func (r *recorder) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

func (r *recorder) GetDealership(_ context.Context, id string) (model.Dealership, error) {
	r.record("GetDealership")
	return model.Dealership{ID: id, Location: time.UTC, Open: 8 * time.Hour, Close: 17 * time.Hour}, nil
}

func (r *recorder) GetVehicle(_ context.Context, id string) (model.Vehicle, error) {
	r.record("GetVehicle")
	return model.Vehicle{ID: id, CustomerID: "C1"}, nil
}

func (r *recorder) GetServiceType(_ context.Context, id string) (model.ServiceType, error) {
	r.record("GetServiceType")
	return model.ServiceType{ID: id, DealershipID: "DL1", RequiredSkill: "oil", Duration: 30 * time.Minute}, nil
}

func (r *recorder) GetAppointment(context.Context, string) (model.Appointment, error) {
	r.record("GetAppointment")
	return model.Appointment{}, model.ErrNotFound
}

func (r *recorder) ListBays(context.Context, string) ([]string, error) {
	r.record("ListBays")
	return []string{"B1"}, nil
}

func (r *recorder) ListQualifiedTechnicians(context.Context, string, string) ([]model.Technician, error) {
	r.record("ListQualifiedTechnicians")
	return []model.Technician{{ID: "T1", WorkStart: 8 * time.Hour, WorkEnd: 17 * time.Hour}}, nil
}

func (r *recorder) ListBusy(context.Context, string, model.Interval) ([]model.Busy, error) {
	r.record("ListBusy")
	return nil, nil
}

func (r *recorder) VehicleHasOverlap(context.Context, string, model.Interval) (bool, error) {
	r.record("VehicleHasOverlap")
	return false, nil
}

func (r *recorder) ClaimKey(context.Context, string, string, time.Time) (*model.StoredKey, error) {
	r.record("ClaimKey")
	return nil, nil
}

func (r *recorder) SaveKeyOutcome(context.Context, string, model.Outcome) error {
	r.record("SaveKeyOutcome")
	return nil
}

func (r *recorder) LockDealership(context.Context, string) error {
	r.record("LockDealership")
	return nil
}

func (r *recorder) LockVehicle(context.Context, string) error {
	r.record("LockVehicle")
	return nil
}

func (r *recorder) InsertAppointment(_ context.Context, a model.Appointment) (model.Appointment, error) {
	r.record("InsertAppointment")
	a.ID = "A1"
	return a, nil
}

// The locks are taken in one order, key, dealership, vehicle, so no two bookings can wait on
// each other in a cycle. They come after the 404 and 422 reads, so a rejection never waits on
// them, and before the availability reads and the insert they protect (D8).
func TestBookCallOrder(t *testing.T) {
	r := &recorder{}
	svc := service.New(r, clock.NewFixed(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)), slog.New(slog.NewTextHandler(io.Discard, nil)))
	out, err := svc.Book(context.Background(), model.BookingRequest{
		DealershipID: "DL1", VehicleID: "V1", ServiceTypeID: "OIL",
		Start: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC),
	}, "8a1f2c3d-0000-4000-8000-000000000001")
	if err != nil || out.Appointment == nil {
		t.Fatalf("Book = %+v, %v; want an appointment", out, err)
	}
	want := []string{
		"ClaimKey",
		"GetDealership", "GetVehicle", "GetServiceType", "ListBays", "ListQualifiedTechnicians",
		"LockDealership", "LockVehicle",
		"VehicleHasOverlap", "ListBusy", "InsertAppointment",
		"SaveKeyOutcome",
	}
	if !slices.Equal(r.calls, want) {
		t.Errorf("calls\n got %v\nwant %v", r.calls, want)
	}
}
