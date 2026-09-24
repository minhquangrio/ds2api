package usageledger

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ds2api/internal/config"
)

type Counters struct {
	Requests         int64 `json:"requests"`
	Success          int64 `json:"success"`
	Errors           int64 `json:"errors"`
	Stopped          int64 `json:"stopped"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	ElapsedMs        int64 `json:"elapsed_ms"`
	ElapsedCount     int64 `json:"elapsed_count"`
	FirstMs          int64 `json:"first_ms"`
	LastMs           int64 `json:"last_ms"`
}

func (c *Counters) Add(r Record) {
	c.Requests++
	switch r.Status {
	case "success":
		c.Success++
	case "error":
		c.Errors++
	case "stopped":
		c.Stopped++
	}
	c.PromptTokens += r.PromptTokens
	c.CompletionTokens += r.CompletionTokens
	c.ReasoningTokens += r.ReasoningTokens
	c.TotalTokens += r.TotalTokens
	if r.ElapsedMs > 0 {
		c.ElapsedMs += r.ElapsedMs
		c.ElapsedCount++
	}
	if r.At > 0 {
		if c.FirstMs == 0 || r.At < c.FirstMs {
			c.FirstMs = r.At
		}
		if r.At > c.LastMs {
			c.LastMs = r.At
		}
	}
}

func (c *Counters) AddCounters(other Counters) {
	c.Requests += other.Requests
	c.Success += other.Success
	c.Errors += other.Errors
	c.Stopped += other.Stopped
	c.PromptTokens += other.PromptTokens
	c.CompletionTokens += other.CompletionTokens
	c.ReasoningTokens += other.ReasoningTokens
	c.TotalTokens += other.TotalTokens
	c.ElapsedMs += other.ElapsedMs
	c.ElapsedCount += other.ElapsedCount
	if other.FirstMs > 0 && (c.FirstMs == 0 || other.FirstMs < c.FirstMs) {
		c.FirstMs = other.FirstMs
	}
	if other.LastMs > c.LastMs {
		c.LastMs = other.LastMs
	}
}

type Bucket struct {
	Counters
	StartMs  int64                `json:"start_ms"`
	Models   map[string]*Counters `json:"models,omitempty"`
	Accounts map[string]*Counters `json:"accounts,omitempty"`
	Callers  map[string]*Counters `json:"callers,omitempty"`
}

type Record struct {
	ID               string `json:"id"`
	CallerID         string `json:"caller_id,omitempty"`
	AccountID        string `json:"account_id,omitempty"`
	Surface          string `json:"surface,omitempty"`
	Model            string `json:"model"`
	At               int64  `json:"at"`
	Stream           bool   `json:"stream"`
	Status           string `json:"status"` // success|error|stopped
	StatusCode       int64  `json:"status_code"`
	ElapsedMs        int64  `json:"elapsed_ms"`
	FinishReason     string `json:"finish_reason,omitempty"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	ReasoningTokens  int64  `json:"reasoning_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	Backfilled       bool   `json:"backfilled,omitempty"`
}

type BackfillMarker struct {
	Done           bool     `json:"done"`
	Version        int      `json:"version,omitempty"`
	Entries        int      `json:"entries,omitempty"`
	SourceRevision int64    `json:"source_revision,omitempty"`
	Totals         Counters `json:"totals,omitempty"`
}

type Store struct {
	mu            sync.Mutex
	path          string
	err           error
	revision      int64
	flushedAt     int64
	totals        Counters
	callers       map[string]*Counters
	minutes       []*Bucket
	recent        []*Record
	hours         map[string][]*Bucket
	days          map[string][]*Bucket
	backfill      BackfillMarker
	dirtyMain     bool
	dirtyHours    map[string]bool
	dirtyDays     map[string]bool
	sinceFlush    int
	wakeFlusher   chan struct{}
	closeCh       chan struct{}
	doneCh        chan struct{}
	flushInterval time.Duration
}

func New(path string) *Store {
	cleanPath := strings.TrimSpace(path)
	flushMs := 5000
	if raw := strings.TrimSpace(os.Getenv("DS2API_USAGE_LEDGER_FLUSH_MS")); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			flushMs = v
		}
	}
	s := &Store{
		path:          cleanPath,
		callers:       make(map[string]*Counters),
		hours:         make(map[string][]*Bucket),
		days:          make(map[string][]*Bucket),
		dirtyHours:    make(map[string]bool),
		dirtyDays:     make(map[string]bool),
		wakeFlusher:   make(chan struct{}, 1),
		closeCh:       make(chan struct{}),
		doneCh:        make(chan struct{}),
		flushInterval: time.Duration(flushMs) * time.Millisecond,
	}
	if cleanPath == "" {
		s.err = errors.New("usage ledger path is required")
		close(s.doneCh)
		return s
	}
	s.mu.Lock()
	if err := s.loadLocked(); err != nil {
		s.err = err
		s.mu.Unlock()
		close(s.doneCh)
		return s
	}
	s.mu.Unlock()

	go s.flusherLoop()
	return s
}

func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) Err() error {
	if s == nil {
		return errors.New("usage ledger store is nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Store) Revision() int64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision
}

func (s *Store) CallerTotalTokens(callerID string) int64 {
	callerID = strings.TrimSpace(callerID)
	if s == nil || callerID == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.callers == nil {
		return 0
	}
	c, ok := s.callers[callerID]
	if !ok || c == nil {
		return 0
	}
	return c.TotalTokens
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return s.err
	}
	select {
	case <-s.closeCh:
		s.mu.Unlock()
		return nil
	default:
		close(s.closeCh)
	}
	s.mu.Unlock()

	<-s.doneCh
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked()
}

func (s *Store) flusherLoop() {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.closeCh:
			return
		case <-ticker.C:
			s.flushIfDirty()
		case <-s.wakeFlusher:
			s.flushIfDirty()
		}
	}
}

func (s *Store) flushIfDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirtyMain && len(s.dirtyHours) == 0 && len(s.dirtyDays) == 0 {
		return
	}
	if err := s.flushLocked(); err != nil {
		config.Logger.Warn("[usage_ledger] flush failed", "path", s.path, "error", err)
	}
}

func (s *Store) recordLocked(rec Record) {
	if rec.At <= 0 {
		rec.At = time.Now().UnixMilli()
	}
	if strings.TrimSpace(rec.Model) == "" {
		rec.Model = "unknown"
	}

	// 1. Totals
	s.totals.Add(rec)
	if rec.CallerID != "" {
		c, ok := s.callers[rec.CallerID]
		if !ok {
			c = &Counters{}
			s.callers[rec.CallerID] = c
		}
		c.Add(rec)
	}

	// 2. Minute bucket (60s)
	minStart := (rec.At / 60000) * 60000
	minBucket := findOrCreateBucket(&s.minutes, minStart, false)
	minBucket.Add(rec)

	// 3. Hour bucket (1h)
	hourShardKey := time.UnixMilli(rec.At).UTC().Format("2006-01")
	hourStart := (rec.At / 3600000) * 3600000
	hourBuckets := s.hours[hourShardKey]
	hourBucket := findOrCreateBucket(&hourBuckets, hourStart, true)
	hourBucket.Add(rec)
	updateDimensions(hourBucket, rec, 32)
	s.hours[hourShardKey] = hourBuckets
	s.dirtyHours[hourShardKey] = true

	// 4. Day bucket (24h)
	dayShardKey := time.UnixMilli(rec.At).UTC().Format("2006-01")
	dayStart := (rec.At / 86400000) * 86400000
	dayBuckets := s.days[dayShardKey]
	dayBucket := findOrCreateBucket(&dayBuckets, dayStart, true)
	dayBucket.Add(rec)
	updateDimensions(dayBucket, rec, 64)
	s.days[dayShardKey] = dayBuckets
	s.dirtyDays[dayShardKey] = true

	// 5. Recent ring buffer (up to 200 items, newest first)
	recCopy := rec
	s.recent = append([]*Record{&recCopy}, s.recent...)
	if len(s.recent) > 200 {
		s.recent = s.recent[:200]
	}

	// 6. Mark dirty
	s.dirtyMain = true
	s.sinceFlush++
	if s.sinceFlush > 5000 {
		select {
		case s.wakeFlusher <- struct{}{}:
		default:
		}
	}
}

func findOrCreateBucket(buckets *[]*Bucket, startMs int64, withDims bool) *Bucket {
	for _, b := range *buckets {
		if b.StartMs == startMs {
			return b
		}
	}
	b := &Bucket{
		StartMs: startMs,
	}
	if withDims {
		b.Models = make(map[string]*Counters)
		b.Accounts = make(map[string]*Counters)
		b.Callers = make(map[string]*Counters)
	}
	*buckets = append(*buckets, b)
	return b
}

func updateDimensions(b *Bucket, rec Record, capLimit int) {
	if b == nil {
		return
	}
	if b.Models == nil {
		b.Models = make(map[string]*Counters)
	}
	addToDim(b.Models, rec.Model, rec, capLimit)

	if rec.AccountID != "" {
		if b.Accounts == nil {
			b.Accounts = make(map[string]*Counters)
		}
		addToDim(b.Accounts, rec.AccountID, rec, capLimit)
	}

	if rec.CallerID != "" {
		if b.Callers == nil {
			b.Callers = make(map[string]*Counters)
		}
		addToDim(b.Callers, rec.CallerID, rec, capLimit)
	}
}

func addToDim(dim map[string]*Counters, key string, rec Record, capLimit int) {
	c, ok := dim[key]
	if !ok {
		if len(dim) >= capLimit {
			key = "__other__"
			c = dim[key]
			if c == nil {
				c = &Counters{}
				dim[key] = c
			}
		} else {
			c = &Counters{}
			dim[key] = c
		}
	}
	c.Add(rec)
}
