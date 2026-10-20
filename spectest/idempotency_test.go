package spectest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

const (
	K1 = "11111111-1111-4111-8111-111111111111"
	K2 = "22222222-2222-4222-8222-222222222222"
	K3 = "33333333-3333-4333-8333-333333333333"
	K4 = "44444444-4444-4444-8444-444444444444"
	K5 = "55555555-5555-4555-8555-555555555555"
)

func TestM15(t *testing.T) {
	e := newEnv(t)
	r1 := e.book("V1", "OIL", "09:00", key(K1), rid("R1"))
	r2 := e.book("V1", "OIL", "09:00", key(K1), rid("R2"))
	e.created(r1, "B1", "T1")
	e.created(r2, "B1", "T1")
	expect(t, "replayed body", r2.Body.String(), r1.Body.String())
	expect(t, "X-Request-ID 1", r1.Header().Get("X-Request-ID"), "R1")
	expect(t, "X-Request-ID 2", r2.Header().Get("X-Request-ID"), "R2")
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 1)
}

func TestM16(t *testing.T) {
	e := newEnv(t)
	e.given("V1", "OIL", "09:00", "B1", "T1")
	r1 := e.book("V2", "BRAKE", "09:00", key(K2))
	r2 := e.book("V2", "BRAKE", "09:00", key(K2))
	e.conflict(r1, "no_technician")
	e.conflict(r2, "no_technician")
	expect(t, "replayed body", r2.Body.String(), r1.Body.String())
}

func TestM18(t *testing.T) {
	e := newEnv(t)
	r1 := e.book("V1", "OIL", "09:00", key(K1))
	e.created(r1, "B1", "T1")
	e.clock.Set(time.Date(2026, 10, 21, 9, 30, 0, 0, hcm))
	r2 := e.book("V1", "OIL", "09:00", key(K1))
	e.created(r2, "B1", "T1")
	expect(t, "replayed body", r2.Body.String(), r1.Body.String())
}

func TestM19(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "09:10", key(K4)), http.StatusUnprocessableEntity)
	e.created(e.book("V1", "OIL", "09:00", key(K4)), "B1", "T1")
}

func TestM20(t *testing.T) {
	e := newEnv(t)
	r1 := e.book("V1", "OIL", "09:00", key(K1))
	e.created(r1, "B1", "T1")
	r2 := e.book("", "", "", key(K1), raw(`{"start":"2026-10-21T02:00Z","service_type_id":"OIL","bay_id":"B2","vehicle_id":"V1","dealership_id":"DL1"}`))
	e.created(r2, "B1", "T1")
	expect(t, "replayed body", r2.Body.String(), r1.Body.String())
}

func TestN20(t *testing.T) {
	e := newEnv(t)
	e.created(e.book("V1", "OIL", "09:00", key(K1)), "B1", "T1")
	e.status(e.book("V2", "OIL", "09:00", key(K1)), http.StatusUnprocessableEntity)
}

func TestN22(t *testing.T) {
	e := newEnv(t)
	e.created(e.book("V1", "OIL", "09:00", key(K1)), "B1", "T1")
	e.status(e.book("", "", "", key(K1), raw(`{"dealership_id":"DL1","service_type_id":"OIL","start":"2026-10-21T09:00:00+07:00"}`)), http.StatusBadRequest)
}

func TestN23(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "09:00", key("abc")), http.StatusBadRequest)
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 0)
}

// Red-team pass 2: an empty Idempotency-Key header is a 400, not a request without a key.
func TestEmptyKey(t *testing.T) {
	e := newEnv(t)
	body := `{"dealership_id":"DL1","vehicle_id":"V1","service_type_id":"OIL","start":"` + startAt("09:00") + `"}`
	r := e.do(http.MethodPost, "/appointments", body, map[string]string{"Idempotency-Key": ""})
	e.status(r, http.StatusBadRequest)
	if msg := e.message(r); !strings.EqualFold(msg, "idempotency-key must be a uuid") {
		t.Errorf("message %q, want idempotency-key must be a uuid", msg)
	}
	expect(t, "appointments", e.count("SELECT count(*) FROM appointments"), 0)
}

// Review Focus 3: the uuid column makes upper and lower case the same key. K1 is all
// digits, so this uses a key with letters in it.
func TestKeyCaseInsensitive(t *testing.T) {
	e := newEnv(t)
	const k = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	r1 := e.book("V1", "OIL", "09:00", key(k))
	r2 := e.book("V1", "OIL", "09:00", key(strings.ToUpper(k)))
	e.created(r1, "B1", "T1")
	e.created(r2, "B1", "T1")
	expect(t, "replayed body", r2.Body.String(), r1.Body.String())
}

// Review Focus 4: a key older than 24 h runs as new, even with a different payload (§6).
func TestExpiredKeyRunsAsNew(t *testing.T) {
	e := newEnv(t)
	_, err := e.db.Exec(context.Background(),
		`INSERT INTO idempotency_keys (key, request_hash, status, body, created_at) VALUES ($1, 'x', 'created', '{}', $2)`,
		K1, defaultNow.Add(-25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	e.created(e.book("V1", "OIL", "09:00", key(K1)), "B1", "T1")
	var hash string
	if err := e.db.QueryRow(context.Background(), `SELECT request_hash FROM idempotency_keys WHERE key = $1`, K1).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	want := model.HashRequest(model.BookingRequest{
		DealershipID: "DL1", VehicleID: "V1", ServiceTypeID: "OIL",
		Start: time.Date(2026, 10, 21, 9, 0, 0, 0, hcm),
	})
	expect(t, "key row hash", hash, want)
}
