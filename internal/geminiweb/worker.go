package geminiweb

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"ds2api/internal/config"
)

type StoreAccountLookup interface {
	Snapshot() config.Config
}

// StartBackgroundRefresher starts the periodic background cookie rotation.
func (r *Runtime) StartBackgroundRefresher(ctx context.Context, store StoreAccountLookup, interval time.Duration) {
	r.workerMu.Lock()
	defer r.workerMu.Unlock()

	if r.workerRun {
		return
	}
	if interval < 60*time.Second {
		interval = 600 * time.Second
	}

	wCtx, cancel := context.WithCancel(ctx)
	r.workerCancel = cancel
	r.workerRun = true
	r.workerWg.Add(1)

	go r.runWorker(wCtx, store, interval)
}

// StopBackgroundRefresher signals the worker to stop and waits for it to exit cleanly.
func (r *Runtime) StopBackgroundRefresher() {
	r.workerMu.Lock()
	if !r.workerRun {
		r.workerMu.Unlock()
		return
	}
	cancel := r.workerCancel
	r.workerRun = false
	r.workerMu.Unlock()

	if cancel != nil {
		cancel()
	}
	r.workerWg.Wait()
}

func (r *Runtime) runWorker(ctx context.Context, store StoreAccountLookup, interval time.Duration) {
	defer r.workerWg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	failures := make(map[string]int)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tickRotate(ctx, store, failures)
		}
	}
}

func (r *Runtime) tickRotate(ctx context.Context, store StoreAccountLookup, failures map[string]int) {
	r.mu.RLock()
	active := make(map[string]*Client, len(r.clients))
	for id, client := range r.clients {
		if client != nil && !client.IsClosed() {
			active[id] = client
		}
	}
	r.mu.RUnlock()

	disabled := make(map[string]bool)
	if store != nil {
		snap := store.Snapshot()
		for _, acc := range snap.Accounts {
			if acc.Disabled {
				disabled[acc.Identifier()] = true
			}
		}
	}

	for id, client := range active {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if disabled[id] || failures[id] >= 3 {
			continue
		}

		jitter := time.Duration(rand.Intn(15)) * time.Second
		if jitter > 0 {
			timer := time.NewTimer(jitter)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}

		err := client.RotateAndInitSession(ctx, RotateSessionOptions{SkipDiscovery: true})
		if err != nil {
			if errors.Is(err, ErrRotationThrottled) {
				continue
			}
			failures[id]++
			config.Logger.Warn("[geminiweb] background cookie refresh failed", "account", id, "error", err, "failures", failures[id])
			if failures[id] >= 3 {
				config.Logger.Warn("[geminiweb] account exceeded max consecutive refresh failures, suspending auto-refresh", "account", id)
			}
		} else {
			failures[id] = 0
			config.Logger.Debug("[geminiweb] background cookie refreshed successfully", "account", id)
		}
	}
}
