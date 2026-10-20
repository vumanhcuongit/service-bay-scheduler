// Package handler is the HTTP layer: decode, 400 checks, call the service, map its result
// to a status and body. It knows nothing about bays, hours or SQL.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/service"
)

type handler struct {
	svc *service.Service
	log *slog.Logger
}

func New(svc *service.Service, log *slog.Logger) http.Handler {
	h := &handler{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /appointments", h.handleCreateAppointment)
	mux.HandleFunc("GET /appointments/{id}", h.handleGetAppointment)
	return requestID(mux)
}

func (h *handler) handleCreateAppointment(w http.ResponseWriter, r *http.Request) {
	req, err := decodeBookingRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := h.svc.Book(r.Context(), req, r.Header.Get("Idempotency-Key"))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeOutcome(w, out)
}

func (h *handler) handleGetAppointment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isUUID(id) {
		writeError(w, http.StatusBadRequest, "id must be a UUID")
		return
	}
	a, err := h.svc.GetAppointment(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAppointmentJSON(a))
}

// writeOutcome renders a booking result. The first response and every replay go through it,
// so their bodies match.
func writeOutcome(w http.ResponseWriter, o model.Outcome) {
	if o.Conflict != nil {
		writeJSON(w, http.StatusConflict, conflictJSON{Reason: string(o.Conflict.Reason), Message: o.Conflict.Message})
		return
	}
	writeJSON(w, http.StatusCreated, toAppointmentJSON(*o.Appointment))
}

func (h *handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	var notFound *model.NotFoundError
	var invalid *model.InvalidError
	switch {
	case errors.As(err, &notFound):
		writeError(w, http.StatusNotFound, notFound.Error())
	case errors.As(err, &invalid):
		writeError(w, http.StatusUnprocessableEntity, invalid.Message)
	case errors.Is(err, model.ErrUnavailable):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "the bay or technician was taken by a concurrent booking; retry with the same Idempotency-Key")
	default:
		h.log.ErrorContext(r.Context(), "request failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

type appointmentJSON struct {
	ID            string `json:"id"`
	DealershipID  string `json:"dealership_id"`
	CustomerID    string `json:"customer_id"`
	VehicleID     string `json:"vehicle_id"`
	ServiceTypeID string `json:"service_type_id"`
	BayID         string `json:"bay_id"`
	TechnicianID  string `json:"technician_id"`
	Start         string `json:"start"`
	End           string `json:"end"`
	Status        string `json:"status"`
}

func toAppointmentJSON(a model.Appointment) appointmentJSON {
	return appointmentJSON{
		ID:            a.ID,
		DealershipID:  a.DealershipID,
		CustomerID:    a.CustomerID,
		VehicleID:     a.VehicleID,
		ServiceTypeID: a.ServiceTypeID,
		BayID:         a.BayID,
		TechnicianID:  a.TechnicianID,
		Start:         a.Start.UTC().Format(time.RFC3339),
		End:           a.End.UTC().Format(time.RFC3339),
		Status:        a.Status,
	}
}

type conflictJSON struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type errorJSON struct {
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorJSON{Message: msg})
}
