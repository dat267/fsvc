package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// discardStdout points os.Stdout at the null device for the duration of fn so
// benchmarks do not print the command's tables.
func discardStdout(fn func()) {
	old := os.Stdout
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		fn()
		return
	}
	os.Stdout = devnull
	defer func() {
		os.Stdout = old
		_ = devnull.Close()
	}()
	fn()
}

// BenchmarkClassifyConversationScan measures the per-ticket conversation scan,
// the phase the worker pool exists for. The mock server holds every request
// for 10ms, so wall time tracks the worker count: 24 tickets cannot finish
// faster than 240ms/workers.
func BenchmarkClassifyConversationScan(b *testing.B) {
	const (
		tickets   = 24
		latency   = 10 * time.Millisecond
		olderDays = 1
	)
	body := ticketListBody(tickets)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/_/tickets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, body)
	})
	mux.HandleFunc("/api/_/tickets/", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(latency)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"conversations":[],"meta":{"count":0}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, workers := range []int{1, 2, 4, 8, 16} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			oldConcurrency, oldNow := concurrency, now
			concurrency = workers
			now = func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) }
			defer func() { concurrency, now = oldConcurrency, oldNow }()

			client := newTestClient(srv.URL)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				discardStdout(func() {
					if err := (&TicketsClassifyCmd{OlderThanDays: olderDays, Page: 1, PerPage: 100}).Run(context.Background(), client); err != nil {
						b.Fatalf("unexpected error: %v", err)
					}
				})
			}
		})
	}
}
