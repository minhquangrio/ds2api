package usageledger

import (
	"os"
	"strings"

	"ds2api/internal/chathistory"
)

const BackfillVersion = 1

func (s *Store) BackfillFromHistory(src *chathistory.Store, force ...bool) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	isForce := len(force) > 0 && force[0]

	if !isForce {
		if s.backfill.Done && s.backfill.Version >= BackfillVersion {
			return nil
		}
		if strings.TrimSpace(os.Getenv("DS2API_USAGE_LEDGER_BACKFILL")) == "0" {
			s.backfill.Done = true
			s.backfill.Version = BackfillVersion
			return s.flushLocked()
		}
	}

	if src == nil {
		s.backfill.Done = true
		s.backfill.Version = BackfillVersion
		s.backfill.Entries = 0
		return s.flushLocked()
	}

	snap, err := src.Snapshot()
	if err != nil {
		return err
	}

	if isForce {
		s.totals = Counters{}
		s.minutes = nil
		s.hours = make(map[string][]*Bucket)
		s.days = make(map[string][]*Bucket)
		s.recent = nil
		s.dirtyHours = make(map[string]bool)
		s.dirtyDays = make(map[string]bool)
		s.dirtyMain = true
	}

	count := 0
	for _, item := range snap.Items {
		at := item.CreatedAt
		if at <= 0 {
			at = item.CompletedAt
		}
		if at <= 0 {
			at = item.UpdatedAt
		}
		if at <= 0 {
			continue
		}

		status := strings.ToLower(strings.TrimSpace(item.Status))
		if status == "streaming" {
			status = "error"
		}
		if status == "" {
			if item.StatusCode >= 200 && item.StatusCode < 400 {
				status = "success"
			} else {
				status = "error"
			}
		}

		model := strings.TrimSpace(item.Model)
		if model == "" {
			model = "unknown"
		}

		rec := Record{
			ID:               item.ID,
			CallerID:         item.CallerID,
			AccountID:        item.AccountID,
			Surface:          item.Surface,
			Model:            model,
			At:               at,
			Stream:           item.Stream,
			Status:           status,
			StatusCode:       int64(item.StatusCode),
			ElapsedMs:        item.ElapsedMs,
			FinishReason:     item.FinishReason,
			PromptTokens:     int64(item.PromptTokens),
			CompletionTokens: int64(item.CompletionTokens),
			ReasoningTokens:  int64(item.ReasoningTokens),
			TotalTokens:      int64(item.TotalTokens),
			Backfilled:       true,
		}

		s.recordLocked(rec)
		count++
	}

	s.backfill = BackfillMarker{
		Done:           true,
		Version:        BackfillVersion,
		Entries:        count,
		SourceRevision: snap.Revision,
		Totals:         s.totals,
	}

	return s.flushLocked()
}
