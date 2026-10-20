package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

// maxBodyBytes caps a booking body.
const maxBodyBytes = 1 << 20

// startLayouts are tried in order. The second takes a start without seconds, e.g. 2026-10-21T02:00Z (M10).
// Both require an offset (N30).
var startLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00"}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isUUID(s string) bool { return uuidPattern.MatchString(s) }

// decodeBookingRequest reads exactly one JSON object with the four schema fields, each a
// non-empty string; unknown fields are ignored (D9). An error is a 400 naming the field.
// An Idempotency-Key that is present but not a UUID, including empty, is also a 400, before
// any lookup (N23); only a missing header means no key.
func decodeBookingRequest(r *http.Request) (model.BookingRequest, error) {
	if _, present := r.Header["Idempotency-Key"]; present && !isUUID(r.Header.Get("Idempotency-Key")) {
		return model.BookingRequest{}, errors.New("Idempotency-Key must be a UUID")
	}
	var fields map[string]json.RawMessage
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	if err := dec.Decode(&fields); err != nil || fields == nil {
		if tooLarge := (*http.MaxBytesError)(nil); errors.As(err, &tooLarge) {
			return model.BookingRequest{}, fmt.Errorf("body must be at most %d bytes", tooLarge.Limit)
		}
		return model.BookingRequest{}, errors.New("body must be a JSON object")
	}
	if _, err := dec.Token(); err != io.EOF {
		return model.BookingRequest{}, errors.New("body must be a single JSON object")
	}

	var req model.BookingRequest
	for _, f := range []struct {
		name string
		dst  *string
	}{
		{"dealership_id", &req.DealershipID},
		{"vehicle_id", &req.VehicleID},
		{"service_type_id", &req.ServiceTypeID},
	} {
		s, err := stringField(fields, f.name)
		if err != nil {
			return model.BookingRequest{}, err
		}
		*f.dst = s
	}
	start, err := stringField(fields, "start")
	if err != nil {
		return model.BookingRequest{}, err
	}
	if req.Start, err = parseStart(start); err != nil {
		return model.BookingRequest{}, err
	}
	return req, nil
}

// stringField returns fields[name] if it is present, a JSON string, and not empty.
func stringField(fields map[string]json.RawMessage, name string) (string, error) {
	v, ok := fields[name]
	if !ok {
		return "", fmt.Errorf("%s is required", name)
	}
	var s *string // null decodes to nil
	if err := json.Unmarshal(v, &s); err != nil || s == nil || *s == "" {
		return "", fmt.Errorf("%s must be a non-empty string", name)
	}
	return *s, nil
}

func parseStart(s string) (time.Time, error) {
	for _, layout := range startLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("start must be RFC 3339 with an offset")
}

type availabilityQuery struct {
	DealershipID, ServiceTypeID string
	// Date carries only the year, month and day; the service reads it in the dealership's location.
	Date time.Time
}

// decodeAvailabilityQuery reads the three required query parameters. An error is a 400 naming one.
func decodeAvailabilityQuery(r *http.Request) (availabilityQuery, error) {
	v := r.URL.Query()
	var q availabilityQuery
	for _, f := range []struct {
		name string
		dst  *string
	}{
		{"dealership_id", &q.DealershipID},
		{"service_type_id", &q.ServiceTypeID},
	} {
		if *f.dst = v.Get(f.name); *f.dst == "" {
			return availabilityQuery{}, fmt.Errorf("%s is required", f.name)
		}
	}
	date := v.Get("date")
	if date == "" {
		return availabilityQuery{}, errors.New("date is required")
	}
	var err error
	if q.Date, err = time.Parse(time.DateOnly, date); err != nil {
		return availabilityQuery{}, errors.New("date must be YYYY-MM-DD")
	}
	return q, nil
}
