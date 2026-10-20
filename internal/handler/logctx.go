package handler

import (
	"context"
	"log/slog"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

// requestInfo is what the request's log lines and metrics share. requestID stores it in the
// context; the route handler fills in DealershipID once decoded and, for a booking, Result.
// One request touches it from one goroutine at a time.
type requestInfo struct {
	ID, Method, Path string
	DealershipID     string
	// Result is the booking_attempts_total label, set by handleCreateAppointment.
	Result model.Result
}

type requestInfoKey struct{}

func infoFrom(ctx context.Context) *requestInfo {
	info, _ := ctx.Value(requestInfoKey{}).(*requestInfo)
	return info
}

// NewContextHandler wraps h so every record logged with a request's context carries
// request_id, method, path and, once known, dealership_id.
func NewContextHandler(h slog.Handler) slog.Handler { return contextHandler{h} }

type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if info := infoFrom(ctx); info != nil {
		r.AddAttrs(slog.String("request_id", info.ID), slog.String("method", info.Method), slog.String("path", info.Path))
		if info.DealershipID != "" {
			r.AddAttrs(slog.String("dealership_id", info.DealershipID))
		}
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{h.Handler.WithGroup(name)}
}
