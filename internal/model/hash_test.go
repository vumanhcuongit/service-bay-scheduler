package model

import (
	"testing"
	"time"
)

func TestHashRequestNormalisesStart(t *testing.T) {
	local := BookingRequest{DealershipID: "DL1", VehicleID: "V1", ServiceTypeID: "OIL", Start: at(9, 0)}
	utc := local
	utc.Start = time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	if HashRequest(local) != HashRequest(utc) {
		t.Error("same instant in +07:00 and UTC gave different hashes")
	}
}

func TestHashRequestDiffersOnField(t *testing.T) {
	a := BookingRequest{DealershipID: "DL1", VehicleID: "V1", ServiceTypeID: "OIL", Start: at(9, 0)}
	b := a
	b.VehicleID = "V2"
	if HashRequest(a) == HashRequest(b) {
		t.Error("different vehicle gave the same hash")
	}
}
