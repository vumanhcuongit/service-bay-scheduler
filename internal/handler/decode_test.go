package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// The 1 MiB limit belongs to decodeBookingRequest itself, so no caller can skip it (Review Focus 1).
func TestDecodeBookingRequestLimitsBody(t *testing.T) {
	body := `{"dealership_id":"DL1","vehicle_id":"V1","service_type_id":"OIL","start":"2026-10-21T09:00:00+07:00","pad":"` +
		strings.Repeat("x", 2<<20) + `"}`
	if _, err := decodeBookingRequest(httptest.NewRequest("POST", "/appointments", strings.NewReader(body))); err == nil {
		t.Fatal("2 MiB body decoded without error")
	}
}
