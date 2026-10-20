// Package handler is the HTTP layer: decode, 400 checks, call the service, map its result
// to a status and body. It knows nothing about bays, hours or SQL.
package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/service"
)

// Config holds the handler's settings. The zero value uses the defaults.
type Config struct {
	// Timeout is the deadline for each request, and so for every DB call it makes (D12).
	// Zero means 5 s.
	Timeout time.Duration
	// Registry receives the booking metrics and is served on GET /metrics. Nil means a new one.
	Registry *prometheus.Registry
}

const defaultTimeout = 5 * time.Second

type handler struct {
	svc *service.Service
	log *slog.Logger
}

func New(svc *service.Service, log *slog.Logger, cfg Config) http.Handler {
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.Registry == nil {
		cfg.Registry = prometheus.NewRegistry()
	}
	h := &handler{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /appointments", h.handleCreateAppointment)
	mux.HandleFunc("GET /appointments/{id}", h.handleGetAppointment)
	mux.HandleFunc("GET /availability", h.handleGetAvailability)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /metrics", promhttp.HandlerFor(cfg.Registry, promhttp.HandlerOpts{}))
	metrics := newBookingMetrics(cfg.Registry)
	return requestID(accessLog(log, metrics.middleware(requestTimeout(cfg.Timeout, mux))))
}

func (h *handler) handleCreateAppointment(w http.ResponseWriter, r *http.Request) {
	info := infoFrom(r.Context())
	if !jsonContentType(r) {
		info.Result = model.ResultInvalid
		h.reject(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	req, err := decodeBookingRequest(r)
	if err != nil {
		info.Result = model.ResultInvalid
		h.reject(w, r, http.StatusBadRequest, err.Error())
		return
	}
	info.DealershipID = req.DealershipID
	out, err := h.svc.Book(r.Context(), req, r.Header.Get("Idempotency-Key"))
	if err != nil {
		switch h.writeServiceError(w, r, err) {
		case http.StatusNotFound, http.StatusUnprocessableEntity:
			info.Result = model.ResultInvalid
		case http.StatusServiceUnavailable:
			info.Result = model.ResultUnavailable
		default:
			info.Result = model.ResultError
		}
		return
	}
	info.Result = out.Result()
	if out.Replayed {
		info.Result = model.ResultReplayed
	}
	writeOutcome(w, out)
}

func (h *handler) handleGetAppointment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !isUUID(id) {
		h.reject(w, r, http.StatusBadRequest, "id must be a UUID")
		return
	}
	a, err := h.svc.GetAppointment(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAppointmentJSON(a))
}

func (h *handler) handleGetAvailability(w http.ResponseWriter, r *http.Request) {
	q, err := decodeAvailabilityQuery(r)
	if err != nil {
		h.reject(w, r, http.StatusBadRequest, err.Error())
		return
	}
	infoFrom(r.Context()).DealershipID = q.DealershipID
	starts, err := h.svc.ListAvailability(r.Context(), q.DealershipID, q.ServiceTypeID, q.Date)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	out := availabilityJSON{
		DealershipID:  q.DealershipID,
		ServiceTypeID: q.ServiceTypeID,
		Date:          q.Date.Format(time.DateOnly),
		StartTimes:    make([]string, len(starts)),
	}
	for i, t := range starts {
		out.StartTimes[i] = t.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, out)
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

// reject writes a 4xx and logs it once, with the message the caller gets, so any rejection can
// be explained from its own lines (design §7). The service does not log rejections.
func (h *handler) reject(w http.ResponseWriter, r *http.Request, status int, msg string) {
	h.log.InfoContext(r.Context(), "request rejected", "result", model.ResultInvalid, "status", status, "message", msg)
	writeError(w, status, msg)
}

// writeServiceError maps a service error to its status, writes it and returns the status.
func (h *handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) int {
	var notFound *model.NotFoundError
	var invalid *model.InvalidError
	switch {
	case errors.As(err, &notFound):
		h.reject(w, r, http.StatusNotFound, notFound.Error())
		return http.StatusNotFound
	case errors.As(err, &invalid):
		h.reject(w, r, http.StatusUnprocessableEntity, invalid.Error())
		return http.StatusUnprocessableEntity
	case errors.Is(err, model.ErrUnavailable):
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "too many concurrent bookings, retry with the same Idempotency-Key")
		return http.StatusServiceUnavailable
	default:
		h.log.ErrorContext(r.Context(), "request failed", "result", model.ResultError, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return http.StatusInternalServerError
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

type availabilityJSON struct {
	DealershipID  string   `json:"dealership_id"`
	ServiceTypeID string   `json:"service_type_id"`
	Date          string   `json:"date"`
	StartTimes    []string `json:"start_times"`
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
