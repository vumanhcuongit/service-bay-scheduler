package spectest

import (
	"net/http"
	"testing"
)

func TestM12(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.status(e.book("V1", "BRAKE", "09:30"), http.StatusCreated)
}

func TestM13(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.status(e.book("V2", "OIL", "09:00"), http.StatusCreated)
}

func TestM14(t *testing.T) {
	e := newEnv(t)
	e.created(e.book("V1", "TIRE", "09:00", dl2()), "B3", "T4")
}

func TestN17(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.conflict(e.book("V1", "OIL", "09:15"), "vehicle_busy")
}

func TestN18(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.given("V2", "OIL", "09:00", "B2", "T2")
	e.conflict(e.book("V1", "OIL", "09:00"), "vehicle_busy")
}

func TestN19(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	e.conflict(e.book("V1", "TIRE", "09:00", dl2()), "vehicle_busy")
}

func TestN21(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "09:00"), http.StatusCreated)
	e.conflict(e.book("V1", "OIL", "09:00"), "vehicle_busy")
}
