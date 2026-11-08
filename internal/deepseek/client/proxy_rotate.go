package client

import (
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"ds2api/internal/config"
	trans "ds2api/internal/deepseek/transport"
)

// A rotating proxy entry resolves to a different exit node over time, so one
// configured proxy owns several transport bundles. Each bundle carries a live
// request count so an endpoint that rotates out is only torn down once the
// requests that started on it have finished.
type proxyUsage struct {
	pending atomic.Int64
}

func (u *proxyUsage) enter() {
	if u != nil {
		u.pending.Add(1)
	}
}

func (u *proxyUsage) leave() {
	if u != nil {
		u.pending.Add(-1)
	}
}

func (u *proxyUsage) inFlight() int64 {
	if u == nil {
		return 0
	}
	return u.pending.Load()
}

// trackedDoer counts requests that are currently reading from a bundle.
type trackedDoer struct {
	inner trans.Doer
	usage *proxyUsage
}

func newTrackedDoer(inner trans.Doer, usage *proxyUsage) trans.Doer {
	if inner == nil {
		return nil
	}
	return trackedDoer{inner: inner, usage: usage}
}

func (d trackedDoer) Do(req *http.Request) (*http.Response, error) {
	d.usage.enter()
	resp, err := d.inner.Do(req)
	if err != nil {
		d.usage.leave()
		return nil, err
	}
	if resp.Body == nil {
		d.usage.leave()
		return resp, nil
	}
	resp.Body = &trackedBody{ReadCloser: resp.Body, usage: d.usage}
	return resp, nil
}

func (d trackedDoer) CloseIdleConnections() {
	if closer, ok := d.inner.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type trackedBody struct {
	io.ReadCloser
	usage *proxyUsage
	once  sync.Once
}

func (b *trackedBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.usage.leave)
	return err
}

func trackRequestClients(bundle requestClients, usage *proxyUsage) requestClients {
	return requestClients{
		regular:   newTrackedDoer(bundle.regular, usage),
		stream:    newTrackedDoer(bundle.stream, usage),
		fallback:  newTrackedDoer(bundle.fallback, usage),
		fallbackS: newTrackedDoer(bundle.fallbackS, usage),
	}
}

// failedProxyClients is handed out when a rotating source cannot provide a
// node. Falling back to a direct connection would expose the host IP of an
// account that is supposed to live behind a proxy, so every call fails loudly
// until the source recovers.
type failedProxyDoer struct {
	err error
}

func (d failedProxyDoer) Do(*http.Request) (*http.Response, error) {
	return nil, d.err
}

func (c *Client) failedProxyClients(proxyID string, cause error) requestClients {
	err := cause
	if err == nil {
		err = errors.New("rotating proxy is unavailable")
	}
	wrapped := &proxyError{proxyID: proxyID, cause: err}
	return c.decorate(requestClients{
		regular:   failedProxyDoer{err: wrapped},
		stream:    failedProxyDoer{err: wrapped},
		fallback:  failedProxyDoer{err: wrapped},
		fallbackS: failedProxyDoer{err: wrapped},
	})
}

// proxyError keeps the failing proxy id visible in API responses and logs.
type proxyError struct {
	proxyID string
	cause   error
}

func (e *proxyError) Error() string {
	return "proxy " + e.proxyID + " unavailable: " + e.cause.Error()
}

func (e *proxyError) Unwrap() error { return e.cause }

// retireGrace is how long a rotated-out endpoint may keep serving requests that
// started before the rotation. Streams can outlive a rotation boundary, so the
// bundle is only closed once its counter reaches zero or this window passes.
const retireGrace = 30 * time.Minute

// sweepRetiredProxyClients collects bundles whose requests have all finished.
// The caller closes them once the client map lock is released.
// Callers must hold c.proxyClientsMu.
func (c *Client) sweepRetiredProxyClients(now time.Time) []requestClients {
	var ready []requestClients
	for key, entry := range c.proxyClients {
		if !entry.retiring {
			continue
		}
		if entry.usage.inFlight() > 0 && now.Sub(entry.retiredAt) < retireGrace {
			continue
		}
		if entry.usage.inFlight() > 0 {
			config.Logger.Warn("[proxy] retiring proxy endpoint still busy",
				"proxy_id", config.NormalizeProxy(entry.proxy).ID, "in_flight", entry.usage.inFlight())
		}
		delete(c.proxyClients, key)
		ready = append(ready, entry.clients)
	}
	return ready
}

// retireProxyClients marks every bundle of one configured proxy other than
// keepKey as retiring, so a rotated exit node is not dropped mid-request.
// Bundles that are already idle are returned for the caller to close.
// Callers must hold c.proxyClientsMu.
func (c *Client) retireProxyClients(proxyID, keepKey string) []requestClients {
	var orphaned []requestClients
	for key, entry := range c.proxyClients {
		if key == keepKey {
			continue
		}
		if config.NormalizeProxy(entry.proxy).ID != proxyID {
			continue
		}
		if entry.retiring {
			continue
		}
		entry.retiring = true
		entry.retiredAt = now()
		c.proxyClients[key] = entry
		if entry.usage.inFlight() == 0 {
			delete(c.proxyClients, key)
			orphaned = append(orphaned, entry.clients)
		}
	}
	return orphaned
}

// now is a small indirection so tests can freeze the retirement clock.
var now = time.Now
