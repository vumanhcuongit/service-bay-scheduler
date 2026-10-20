package spectest

import (
	"net/http"
	"testing"
)

func TestM24(t *testing.T) {
	e := newEnv(t)
	r := e.book("V1", "OIL", "09:00")
	a := e.created(r, "B1", "T1")
	g := e.get("/appointments/" + a.ID)
	e.status(g, http.StatusOK)
	expect(t, "GET body", g.Body.String(), r.Body.String())
	expect(t, "status", a.Status, "confirmed")
}

func TestN35(t *testing.T) {
	e := newEnv(t)
	e.status(e.get("/appointments/00000000-0000-0000-0000-000000000000"), http.StatusNotFound)
}

// Review Focus 2: a non-UUID id is a 400, never a failed uuid cast in Postgres.
func TestGetAppointmentNonUUID(t *testing.T) {
	e := newEnv(t)
	e.status(e.get("/appointments/abc"), http.StatusBadRequest)
}
