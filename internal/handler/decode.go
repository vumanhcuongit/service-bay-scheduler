package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isUUID(s string) bool { return uuidPattern.MatchString(s) }

// decodeBookingRequest reads the four schema fields; unknown fields are ignored (D9).
// Task 4 adds the full 400 rules.
func decodeBookingRequest(r *http.Request) (model.BookingRequest, error) {
	var body struct {
		DealershipID  string `json:"dealership_id"`
		VehicleID     string `json:"vehicle_id"`
		ServiceTypeID string `json:"service_type_id"`
		Start         string `json:"start"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return model.BookingRequest{}, fmt.Errorf("body must be a JSON object: %w", err)
	}
	start, err := time.Parse(time.RFC3339Nano, body.Start)
	if err != nil {
		return model.BookingRequest{}, fmt.Errorf("start must be RFC 3339 with an offset")
	}
	return model.BookingRequest{
		DealershipID:  body.DealershipID,
		VehicleID:     body.VehicleID,
		ServiceTypeID: body.ServiceTypeID,
		Start:         start,
	}, nil
}
