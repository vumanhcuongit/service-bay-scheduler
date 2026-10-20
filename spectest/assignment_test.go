package spectest

import "testing"

func TestM1(t *testing.T) {
	e := newEnv(t)
	a := e.created(e.book("V1", "OIL", "09:00"), "B1", "T1")
	expect(t, "customer_id", a.CustomerID, "C1")
	expect(t, "start", a.Start, "2026-10-21T02:00:00Z")
	expect(t, "end", a.End, "2026-10-21T02:30:00Z")
}

func TestM2(t *testing.T) {
	e := newEnv(t)
	a := e.created(e.book("V1", "BRAKE", "09:00"), "B1", "T1")
	expect(t, "end", a.End, "2026-10-21T03:00:00Z")
}

func TestM3(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.created(e.book("V2", "OIL", "09:00"), "B2", "T2")
}

func TestM4(t *testing.T) {
	e := newEnv(t)
	a := e.created(e.book("V1", "EV_CHECK", "09:00"), "B1", "T2")
	expect(t, "end", a.End, "2026-10-21T02:45:00Z")
}

func TestM5(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.created(e.book("V2", "OIL", "09:30"), "B1", "T1")
}

func TestM6(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "BRAKE", "09:00", "B1", "T1")
	e.created(e.book("V2", "OIL", "09:00"), "B2", "T2")
}

func TestM7(t *testing.T) {
	e := newEnv(t)
	e.created(e.book("V1", "OIL", "09:00", extra(map[string]any{"bay_id": "B2", "technician_id": "T2"})), "B1", "T1")
}
