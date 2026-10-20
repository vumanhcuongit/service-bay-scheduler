package spectest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var idFields = []string{"dealership_id", "vehicle_id", "service_type_id"}

// validBody is book(V1, OIL, 09:00) as a field map, for tests that break one field.
func validBody() map[string]any {
	return map[string]any{
		"dealership_id":   "DL1",
		"vehicle_id":      "V1",
		"service_type_id": "OIL",
		"start":           startAt("09:00"),
	}
}

func (e *env) bookFields(fields map[string]any) *httptest.ResponseRecorder {
	e.t.Helper()
	b, err := json.Marshal(fields)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.book("", "", "", raw(string(b)))
}

func TestN26(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "09:00", dealership("DL9")), http.StatusNotFound)
}

func TestN27(t *testing.T) {
	for _, field := range append(idFields, "start") {
		t.Run(field, func(t *testing.T) {
			e := newEnv(t)
			body := validBody()
			delete(body, field)
			r := e.bookFields(body)
			e.status(r, http.StatusBadRequest)
			if msg := e.message(r); !strings.Contains(msg, field) {
				t.Errorf("message %q does not name %s", msg, field)
			}
		})
	}
}

func TestN28(t *testing.T) {
	for _, field := range idFields {
		for name, value := range map[string]any{"empty": "", "null": nil, "number": 42} {
			t.Run(field+"/"+name, func(t *testing.T) {
				e := newEnv(t)
				body := validBody()
				body[field] = value
				r := e.bookFields(body)
				e.status(r, http.StatusBadRequest)
				if msg := e.message(r); !strings.Contains(msg, field) {
					t.Errorf("message %q does not name %s", msg, field)
				}
			})
		}
	}
}

func TestN29(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "tomorrow"), http.StatusBadRequest)
}

func TestN30(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "OIL", "2026-10-21T09:00"), http.StatusBadRequest)
}

func TestN31(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V9", "OIL", "09:00"), http.StatusNotFound)
}

func TestN32(t *testing.T) {
	e := newEnv(t)
	e.status(e.book("V1", "X9", "09:00"), http.StatusNotFound)
}

func TestN33(t *testing.T) {
	e := newEnv(t)
	r := e.book("V9", "X9", "09:00", dealership("DL9"))
	e.status(r, http.StatusNotFound)
	if msg := e.message(r); !strings.Contains(msg, "dealership") {
		t.Errorf("message %q does not name the dealership", msg)
	}
}

// TestMalformedBody is Review Focus 1: a body that is not one JSON object is a 400, never a 500.
func TestMalformedBody(t *testing.T) {
	valid, _ := json.Marshal(validBody())
	padded := validBody()
	padded["pad"] = strings.Repeat("x", 2<<20) // unknown fields are ignored, so only the size limit can reject it
	big, _ := json.Marshal(padded)
	for name, body := range map[string]string{
		"array":     `[]`,
		"truncated": `{"dealership_id":`,
		"trailing":  string(valid) + `{}`,
		"brace":     string(valid) + `}`,
		"2MiB":      string(big),
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.status(e.book("", "", "", raw(body)), http.StatusBadRequest)
		})
	}
}
