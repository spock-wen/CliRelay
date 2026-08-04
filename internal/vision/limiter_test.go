package vision

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConcurrencyLimiterCapsInFlight(t *testing.T) {
	lim := NewConcurrencyLimiter(3)
	var maxInFlight atomic.Int32
	var cur atomic.Int32
	var wg sync.WaitGroup
	ctx := context.Background()
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = lim.Run(ctx, func() error {
				v := cur.Add(1)
				for {
					m := maxInFlight.Load()
					if v <= m || maxInFlight.CompareAndSwap(m, v) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				cur.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()
	if got := maxInFlight.Load(); got > 3 {
		t.Fatalf("max in-flight = %d, want <= 3", got)
	}
}

func TestConcurrencyLimiterContextCancel(t *testing.T) {
	lim := NewConcurrencyLimiter(1)
	blocked := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = lim.Run(context.Background(), func() error {
			close(blocked)
			<-release
			return nil
		})
	}()
	<-blocked
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := lim.Run(ctx, func() error { return nil })
	if err == nil {
		t.Fatal("expected context deadline exceeded, got nil")
	}
	close(release)
}

func TestKeyPoolRoundRobinAndConcurrency(t *testing.T) {
	p := NewKeyPool([]string{"k1", "k2"}, 1, time.Minute)
	var got []string
	for i := 0; i < 4; i++ {
		k, release, ok := p.Acquire()
		if !ok {
			t.Fatalf("acquire %d failed", i)
		}
		got = append(got, k)
		release()
	}
	if len(got) != 4 {
		t.Fatalf("acquired %d, want 4", len(got))
	}
	_, rel1, _ := p.Acquire()
	_, rel2, _ := p.Acquire()
	if k, _, ok := p.Acquire(); ok {
		t.Fatalf("expected no key available, got %q", k)
	}
	rel1()
	rel2()
}

func TestKeyPoolMarkUnavailable(t *testing.T) {
	p := NewKeyPool([]string{"k1"}, 2, time.Hour)
	p.MarkUnavailable("k1")
	if _, _, ok := p.Acquire(); ok {
		t.Fatal("expected acquire to fail after mark unavailable")
	}
}

func TestKeyPoolCooldownExpires(t *testing.T) {
	p := NewKeyPool([]string{"k1"}, 2, 10*time.Millisecond)
	p.MarkUnavailable("k1")
	time.Sleep(15 * time.Millisecond)
	if _, _, ok := p.Acquire(); !ok {
		t.Fatal("expected acquire to succeed after cooldown expired")
	}
}
