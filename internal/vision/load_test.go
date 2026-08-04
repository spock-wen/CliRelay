//go:build load

package vision

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRecognizer100Concurrent(t *testing.T) {
	var inFlight atomic.Int64
	var maxInFlight atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			m := maxInFlight.Load()
			if v <= m || maxInFlight.CompareAndSwap(m, v) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"SUMMARY: ok"}}]}`))
	}))
	defer srv.Close()

	r := NewRecognizer(RecognizerConfig{
		BaseURL: srv.URL, APIKeys: []string{"k1", "k2", "k3", "k4", "k5"},
		Model: "m", MaxConcurrency: 100, PerKeyConcurrency: 20, KeyCooldown: time.Minute,
		Timeout: 30 * time.Second, Retries: 0,
		Preprocess: DefaultPreprocessConfig(), AnalyzeTimeout: 5 * time.Second,
	})
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	img := base64Of(t, makeTestJPEG(t, 512, 512))
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Analyze(ctx, AnalyzeRequest{ImageData: img, MIMEType: "image/jpeg"})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent analyze failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("100 concurrent took %v, too slow", elapsed)
	}
	if got := maxInFlight.Load(); got > 100 {
		t.Fatalf("max in-flight = %d > 100", got)
	}
}
