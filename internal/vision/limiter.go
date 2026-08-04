package vision

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// ConcurrencyLimiter is a bounded semaphore with context-aware acquire.
type ConcurrencyLimiter struct {
	sem *semaphore.Weighted
}

func NewConcurrencyLimiter(max int) *ConcurrencyLimiter {
	if max <= 0 {
		max = 1
	}
	return &ConcurrencyLimiter{sem: semaphore.NewWeighted(int64(max))}
}

func (l *ConcurrencyLimiter) Run(ctx context.Context, fn func() error) error {
	if err := l.sem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer l.sem.Release(1)
	return fn()
}

// KeyPool round-robins across API keys, capping per-key concurrency and
// cooling down keys that hit errors/429s.
type KeyPool struct {
	mu            sync.Mutex
	keys          []string
	inFlight      []int
	cooldownUntil []time.Time
	perKey        int
	cooldown      time.Duration
	cursor        int
}

func NewKeyPool(keys []string, perKeyConcurrency int, cooldown time.Duration) *KeyPool {
	if perKeyConcurrency <= 0 {
		perKeyConcurrency = 1
	}
	return &KeyPool{
		keys:          append([]string(nil), keys...),
		inFlight:      make([]int, len(keys)),
		cooldownUntil: make([]time.Time, len(keys)),
		perKey:        perKeyConcurrency,
		cooldown:      cooldown,
	}
}

func (p *KeyPool) Acquire() (string, func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	n := len(p.keys)
	for i := 0; i < n; i++ {
		idx := (p.cursor + i) % n
		if p.cooldownUntil[idx].After(now) {
			continue
		}
		if p.inFlight[idx] < p.perKey {
			p.inFlight[idx]++
			p.cursor = (idx + 1) % n
			key := p.keys[idx]
			var done bool
			return key, func() {
				p.mu.Lock()
				defer p.mu.Unlock()
				if done {
					return
				}
				done = true
				if p.inFlight[idx] > 0 {
					p.inFlight[idx]--
				}
			}, true
		}
	}
	return "", nil, false
}

func (p *KeyPool) MarkUnavailable(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, k := range p.keys {
		if k == key {
			p.cooldownUntil[i] = time.Now().Add(p.cooldown)
			return
		}
	}
}
