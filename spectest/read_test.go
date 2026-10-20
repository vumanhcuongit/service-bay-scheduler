package spectest

import (
	"encoding/json"
	"net/http"
	"regexp"
	"testing"
)

func TestM24(t *testing.T) {
	e := newEnv(t)
	r := e.book("V1", "OIL", "09:00")
	a := e.created(r, "B1", "T1")
	g := e.get("/appointments/" + a.ID)
	e.status(g, http.StatusOK)

	// Each field the spec lists, read from the GET body: both bodies come from the same
	// mapper, so comparing them alone would not catch a mapping bug.
	var got appointment
	if err := json.Unmarshal(g.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode GET body %q: %v", g.Body.String(), err)
	}
	if !uuidShape.MatchString(got.ID) {
		t.Errorf("id = %q, want a UUID", got.ID)
	}
	expect(t, "dealership_id", got.DealershipID, "DL1")
	expect(t, "customer_id", got.CustomerID, "C1")
	expect(t, "vehicle_id", got.VehicleID, "V1")
	expect(t, "service_type_id", got.ServiceTypeID, "OIL")
	expect(t, "bay_id", got.BayID, "B1")
	expect(t, "technician_id", got.TechnicianID, "T1")
	expect(t, "start", got.Start, "2026-10-21T02:00:00Z")
	expect(t, "end", got.End, "2026-10-21T02:30:00Z")
	expect(t, "status", got.Status, "confirmed")
	expect(t, "GET body equals the 201 body", g.Body.String(), r.Body.String())
}

var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestN35(t *testing.T) {
	e := newEnv(t)
	e.status(e.get("/appointments/00000000-0000-0000-0000-000000000000"), http.StatusNotFound)
}

// Review Focus 2: a non-UUID id is a 400, never a failed uuid cast in Postgres.
func TestGetAppointmentNonUUID(t *testing.T) {
	e := newEnv(t)
	e.status(e.get("/appointments/abc"), http.StatusBadRequest)
}
