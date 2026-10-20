package spectest

import (
	"net/http"
	"testing"
	"time"
)

func TestM8(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "08:00"), http.StatusCreated)
}

func TestM9(t *testing.T) {
	e := newEnv(t)
	a := e.created(e.book("V1", "BRAKE", "16:00"), "B1", "T1")
	expect(t, "end", a.End, "2026-10-21T10:00:00Z")
}

func TestM10(t *testing.T) {
	e := newEnv(t)
	a := e.created(e.book("V1", "OIL", "2026-10-21T02:00Z"), "B1", "T1")
	expect(t, "customer_id", a.CustomerID, "C1")
	expect(t, "start", a.Start, "2026-10-21T02:00:00Z")
	expect(t, "end", a.End, "2026-10-21T02:30:00Z")
}

func TestM11(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "2026-10-21T09:00:00.000+07:00"), http.StatusCreated)
}

func TestN1(t *testing.T) { unprocessable(t, "V1", "OIL", "07:45") }
func TestN2(t *testing.T) { unprocessable(t, "V1", "OIL", "17:00") }
func TestN3(t *testing.T) { unprocessable(t, "V1", "EV_CHECK", "16:30") }
func TestN4(t *testing.T) { unprocessable(t, "V1", "OIL", "09:10") }
func TestN5(t *testing.T) { unprocessable(t, "V1", "OIL", "09:00:30") }
func TestN6(t *testing.T) { unprocessable(t, "V1", "OIL", "2026-10-20T09:00:00+07:00") }

func TestN7(t *testing.T) {
	e := newEnv(t)
	e.clock.Set(time.Date(2026, 10, 21, 9, 0, 0, 0, hcm))
	e.status(e.book("V1", "OIL", "09:00"), http.StatusUnprocessableEntity)
}

func TestN24(t *testing.T) { unprocessable(t, "V1", "TIRE", "09:00") }

func TestN25(t *testing.T) { unprocessable(t, "V1", "OIL", "09:00", dl2()) }

func unprocessable(t *testing.T, vehicle, serviceType, at string, opts ...opt) {
	t.Helper()
	e := newEnv(t)
	e.status(e.book(vehicle, serviceType, at, opts...), http.StatusUnprocessableEntity)
}
