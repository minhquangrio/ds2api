package usageledger

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"ds2api/internal/assistantturn"
	"ds2api/internal/chathistory"
)

type Meta struct {
	CallerID      string
	AccountID     string
	Surface       string
	Model         string
	Stream        bool
	Prompt        string // final upstream prompt
	RefFileTokens int
}

type Outcome struct {
	Status       string // success|error|stopped
	StatusCode   int
	FinishReason string
	Thinking     string
	Content      string
	Usage        map[string]any // nil => auto infer
}

type Recorder struct {
	store     *Store
	meta      Meta
	startedAt time.Time
	once      sync.Once
}

func Begin(store *Store, meta Meta) *Recorder {
	if store == nil || store.err != nil {
		return nil
	}
	return &Recorder{
		store:     store,
		meta:      meta,
		startedAt: time.Now(),
	}
}

func (r *Recorder) Record(out Outcome) {
	if r == nil || r.store == nil {
		return
	}
	r.once.Do(func() {
		nowMs := time.Now().UnixMilli()
		elapsed := time.Since(r.startedAt).Milliseconds()
		if elapsed <= 0 {
			elapsed = 1
		}

		status := strings.ToLower(strings.TrimSpace(out.Status))
		if status == "" {
			if out.StatusCode >= 200 && out.StatusCode < 400 {
				status = "success"
			} else {
				status = "error"
			}
		}

		promptTokens, completionTokens, reasoningTokens, totalTokens := chathistory.ExtractTokenCounts(out.Usage)
		if totalTokens == 0 {
			genUsage := assistantturn.GenericUsage(assistantturn.BuildUsage(
				r.meta.Model,
				r.meta.Prompt,
				out.Thinking,
				out.Content,
				r.meta.RefFileTokens,
			))
			promptTokens, completionTokens, reasoningTokens, totalTokens = chathistory.ExtractTokenCounts(genUsage)
		}

		model := strings.TrimSpace(r.meta.Model)
		if model == "" {
			model = "unknown"
		}

		rec := Record{
			ID:               generateRecordID(nowMs),
			CallerID:         strings.TrimSpace(r.meta.CallerID),
			AccountID:        strings.TrimSpace(r.meta.AccountID),
			Surface:          strings.TrimSpace(r.meta.Surface),
			Model:            model,
			At:               nowMs,
			Stream:           r.meta.Stream,
			Status:           status,
			StatusCode:       int64(out.StatusCode),
			ElapsedMs:        elapsed,
			FinishReason:     strings.TrimSpace(out.FinishReason),
			PromptTokens:     int64(promptTokens),
			CompletionTokens: int64(completionTokens),
			ReasoningTokens:  int64(reasoningTokens),
			TotalTokens:      int64(totalTokens),
			Backfilled:       false,
		}

		r.store.mu.Lock()
		r.store.recordLocked(rec)
		r.store.mu.Unlock()
	})
}

func generateRecordID(at int64) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("usage_%d_%s", at, hex.EncodeToString(b))
}
