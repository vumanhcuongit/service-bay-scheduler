package spectest

import "testing"

func TestN8(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.given("V2", "OIL", "09:00", "B2", "T2")
	e.conflict(e.book("V3", "OIL", "09:00"), "no_bay")
}

func TestN9(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.conflict(e.book("V2", "BRAKE", "09:00"), "no_technician")
}

func TestN10(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:30", "B1", "T1")
	e.conflict(e.book("V2", "BRAKE", "09:00"), "no_technician")
}

func TestN11(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:30", "B1", "T1")
	e.given("V2", "OIL", "09:30", "B2", "T2")
	e.conflict(e.book("V3", "OIL", "09:15"), "no_bay")
}

func TestN12(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.given("V2", "OIL", "09:00", "B2", "T2")
	e.conflict(e.book("V3", "BRAKE", "09:00"), "no_bay")
}

func TestN13(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "TIRE", "09:00", "B3", "T4")
	e.conflict(e.book("V2", "TIRE", "09:00", dl2()), "no_bay")
}

func TestN14(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "EV_CHECK", "09:00", "B1", "T2")
	e.conflict(e.book("V2", "EV_CHECK", "09:00"), "no_technician")
}

func TestN15(t *testing.T) {
	e := newEnv(t)
	e.conflict(e.book("V1", "TRANSMISSION", "09:00"), "no_technician")
}

func TestN16(t *testing.T) {
	e := newEnv(t)
	e.conflict(e.book("V1", "EV_CHECK", "14:00"), "no_technician")
}
