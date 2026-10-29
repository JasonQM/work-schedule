package platform

import (
	"context"
	"sync"
	"time"
)

// A single lazy poller serves all open dashboards. Buffered notifications coalesce bursts.
type revisionHub struct {
	mu         sync.Mutex
	read       func(context.Context) (int64, error)
	interval   time.Duration
	watchers   map[chan int64]struct{}
	cancel     context.CancelFunc
	generation uint64
	latest     int64
}

func newRevisionHub(read func(context.Context) (int64, error), interval time.Duration) *revisionHub {
	return &revisionHub{read: read, interval: interval, watchers: map[chan int64]struct{}{}, latest: -1}
}
func (h *revisionHub) subscribe() (<-chan int64, func()) {
	h.mu.Lock()
	ch := make(chan int64, 1)
	h.watchers[ch] = struct{}{}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		h.generation++
		gen := h.generation
		go h.run(ctx, gen)
	}
	if h.latest >= 0 {
		ch <- h.latest
	}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.watchers, ch)
			if len(h.watchers) == 0 && h.cancel != nil {
				h.cancel()
				h.cancel = nil
				h.latest = -1
			}
			h.mu.Unlock()
		})
	}
}
func (h *revisionHub) run(ctx context.Context, generation uint64) {
	tick := time.NewTicker(h.interval)
	defer tick.Stop()
	for {
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		revision, err := h.read(readCtx)
		cancel()
		if err == nil {
			h.mu.Lock()
			if h.generation != generation || ctx.Err() != nil {
				h.mu.Unlock()
				return
			}
			if revision != h.latest {
				h.latest = revision
				for ch := range h.watchers {
					select {
					case <-ch:
					default:
					}
					select {
					case ch <- revision:
					default:
					}
				}
			}
			h.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
