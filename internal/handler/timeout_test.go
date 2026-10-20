package handler_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/clock"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/handler"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/repository"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/service"
	"github.com/vumanhcuongit/service-bay-scheduler/internal/testdb"
)

// The request deadline, not a session setting, bounds DB calls (D12), so it also works
// behind a transaction-mode pooler (design §8). A held lock must end in a 500, not a hang.
func TestRequestTimeoutBoundsDBWait(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t)

	locker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = locker.Rollback(ctx) }()
	if _, err := locker.Exec(ctx, "LOCK TABLE appointments IN EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.New(repository.New(pool), clock.NewFixed(time.Date(2026, 10, 20, 5, 0, 0, 0, time.UTC)), log)
	h := handler.New(svc, log, handler.Config{Timeout: 200 * time.Millisecond})

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/appointments", strings.NewReader(
			`{"dealership_id":"DL1","vehicle_id":"V1","service_type_id":"OIL","start":"2026-10-21T09:00:00+07:00"}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		done <- rec
	}()

	select {
	case rec := <-done:
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status %d, want 500; body %s", rec.Code, rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request still waiting on the lock after 3 s; the 200 ms timeout did not apply")
	}
}
