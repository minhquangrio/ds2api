package usageledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ds2api/internal/util"
)

type mainFilePayload struct {
	Version   int                  `json:"version"`
	Revision  int64                `json:"revision"`
	FlushedAt int64                `json:"flushed_at"`
	Totals    Counters             `json:"totals"`
	Callers   map[string]*Counters `json:"callers,omitempty"`
	Minutes   []*Bucket            `json:"minutes"`
	Recent    []*Record            `json:"recent"`
	Backfill  BackfillMarker       `json:"backfill"`
}

type shardFilePayload struct {
	Shard   string    `json:"shard"`
	Buckets []*Bucket `json:"buckets"`
}

func (s *Store) hourDir() string {
	return s.path + ".h"
}

func (s *Store) dayDir() string {
	return s.path + ".d"
}

func (s *Store) hourShardPath(shardKey string) string {
	return filepath.Join(s.hourDir(), shardKey+".json")
}

func (s *Store) dayShardPath(shardKey string) string {
	return filepath.Join(s.dayDir(), shardKey+".json")
}

func (s *Store) flushLocked() error {
	nowMs := time.Now().UnixMilli()

	// 1. Prune expired buckets
	minCutoff := nowMs - MinuteRetentionMs
	var prunedMinutes []*Bucket
	for _, b := range s.minutes {
		if b.StartMs+MinuteWidthMs >= minCutoff {
			prunedMinutes = append(prunedMinutes, b)
		}
	}
	s.minutes = prunedMinutes

	hourCutoff := nowMs - HourRetentionMs
	for shardKey, buckets := range s.hours {
		var kept []*Bucket
		for _, b := range buckets {
			if b.StartMs+HourWidthMs >= hourCutoff {
				kept = append(kept, b)
			}
		}
		if len(kept) == 0 {
			delete(s.hours, shardKey)
			delete(s.dirtyHours, shardKey)
			filePath := s.hourShardPath(shardKey)
			if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove empty hour shard: %w", err)
			}
		} else {
			s.hours[shardKey] = kept
		}
	}

	dayCutoff := nowMs - DayRetentionMs
	for shardKey, buckets := range s.days {
		var kept []*Bucket
		for _, b := range buckets {
			if b.StartMs+DayWidthMs >= dayCutoff {
				kept = append(kept, b)
			}
		}
		if len(kept) == 0 {
			delete(s.days, shardKey)
			delete(s.dirtyDays, shardKey)
			filePath := s.dayShardPath(shardKey)
			if err := os.Remove(filePath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove empty day shard: %w", err)
			}
		} else {
			s.days[shardKey] = kept
		}
	}

	// 2. Write dirty hour shards
	for shardKey := range s.dirtyHours {
		buckets := s.hours[shardKey]
		if len(buckets) > 0 {
			payload := shardFilePayload{
				Shard:   shardKey,
				Buckets: buckets,
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal hour shard %s: %w", shardKey, err)
			}
			if err := util.WriteFileAtomic(s.hourShardPath(shardKey), data); err != nil {
				return fmt.Errorf("write hour shard %s: %w", shardKey, err)
			}
		}
	}
	s.dirtyHours = make(map[string]bool)

	// 3. Write dirty day shards
	for shardKey := range s.dirtyDays {
		buckets := s.days[shardKey]
		if len(buckets) > 0 {
			payload := shardFilePayload{
				Shard:   shardKey,
				Buckets: buckets,
			}
			data, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal day shard %s: %w", shardKey, err)
			}
			if err := util.WriteFileAtomic(s.dayShardPath(shardKey), data); err != nil {
				return fmt.Errorf("write day shard %s: %w", shardKey, err)
			}
		}
	}
	s.dirtyDays = make(map[string]bool)

	// 4. Write main file
	s.revision++
	s.flushedAt = nowMs
	mainPayload := mainFilePayload{
		Version:   1,
		Revision:  s.revision,
		FlushedAt: s.flushedAt,
		Totals:    s.totals,
		Callers:   s.callers,
		Minutes:   s.minutes,
		Recent:    s.recent,
		Backfill:  s.backfill,
	}
	mainData, err := json.Marshal(mainPayload)
	if err != nil {
		return fmt.Errorf("marshal main usage ledger: %w", err)
	}
	if err := util.WriteFileAtomic(s.path, mainData); err != nil {
		return fmt.Errorf("write main usage ledger: %w", err)
	}

	s.dirtyMain = false
	s.sinceFlush = 0
	return nil
}

func (s *Store) loadLocked() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.hours = make(map[string][]*Bucket)
			s.days = make(map[string][]*Bucket)
			return nil
		}
		return fmt.Errorf("read main usage ledger: %w", err)
	}

	var payload mainFilePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return fmt.Errorf("unmarshal main usage ledger: %w", err)
	}

	s.revision = payload.Revision
	s.flushedAt = payload.FlushedAt
	s.totals = payload.Totals
	if payload.Callers != nil {
		s.callers = payload.Callers
	} else {
		s.callers = make(map[string]*Counters)
	}
	s.minutes = payload.Minutes
	s.recent = payload.Recent
	s.backfill = payload.Backfill
	s.hours = make(map[string][]*Bucket)
	s.days = make(map[string][]*Bucket)

	// Load hour shards
	if entries, err := os.ReadDir(s.hourDir()); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			shardKey := strings.TrimSuffix(entry.Name(), ".json")
			shardData, err := os.ReadFile(filepath.Join(s.hourDir(), entry.Name()))
			if err != nil {
				return fmt.Errorf("read hour shard %s: %w", shardKey, err)
			}
			var sp shardFilePayload
			if err := json.Unmarshal(shardData, &sp); err != nil {
				return fmt.Errorf("unmarshal hour shard %s: %w", shardKey, err)
			}
			s.hours[shardKey] = sp.Buckets
		}
	}

	// Load day shards
	if entries, err := os.ReadDir(s.dayDir()); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			shardKey := strings.TrimSuffix(entry.Name(), ".json")
			shardData, err := os.ReadFile(filepath.Join(s.dayDir(), entry.Name()))
			if err != nil {
				return fmt.Errorf("read day shard %s: %w", shardKey, err)
			}
			var sp shardFilePayload
			if err := json.Unmarshal(shardData, &sp); err != nil {
				return fmt.Errorf("unmarshal day shard %s: %w", shardKey, err)
			}
			s.days[shardKey] = sp.Buckets
		}
	}

	return nil
}
