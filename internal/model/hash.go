package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// HashRequest hashes the canonical payload of a booking: the four schema fields in a
// fixed order, start in UTC with its fractional seconds. Unknown fields, key order and whitespace never reach it (D9).
func HashRequest(r BookingRequest) string {
	canonical, _ := json.Marshal(struct {
		DealershipID  string `json:"dealership_id"`
		ServiceTypeID string `json:"service_type_id"`
		Start         string `json:"start"`
		VehicleID     string `json:"vehicle_id"`
	}{r.DealershipID, r.ServiceTypeID, r.Start.UTC().Format(time.RFC3339Nano), r.VehicleID})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}
