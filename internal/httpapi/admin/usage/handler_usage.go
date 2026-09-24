package usage

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"ds2api/internal/usageledger"
)

func LedgerETag(revision int64) string {
	return fmt.Sprintf(`W/"usage-ledger-%d"`, revision)
}

func (h *Handler) getUsage(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.UsageLedger == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "usage ledger is not configured"})
		return
	}

	revision := h.UsageLedger.Revision()
	etag := LedgerETag(revision)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")

	ifNoneMatch := strings.TrimSpace(r.Header.Get("If-None-Match"))
	if ifNoneMatch != "" && ifNoneMatch == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	q := r.URL.Query()
	rangeKey := strings.TrimSpace(q.Get("range"))
	if rangeKey == "" {
		rangeKey = "all"
	}
	switch rangeKey {
	case "15m", "1h", "24h", "7d", "30d", "all", "custom":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid range: " + rangeKey})
		return
	}

	var startMs, endMs int64
	if rangeKey == "custom" {
		rawStart := strings.TrimSpace(q.Get("start"))
		rawEnd := strings.TrimSpace(q.Get("end"))
		if rawStart == "" || rawEnd == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "custom range requires start and end epoch milliseconds"})
			return
		}
		s, errS := strconv.ParseInt(rawStart, 10, 64)
		e, errE := strconv.ParseInt(rawEnd, 10, 64)
		if errS != nil || errE != nil || s <= 0 || e <= 0 || s >= e {
			writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid custom range start/end"})
			return
		}
		startMs = s
		endMs = e
	}

	top := 10
	if rawTop := strings.TrimSpace(q.Get("top")); rawTop != "" {
		if v, err := strconv.Atoi(rawTop); err == nil && v > 0 {
			top = v
		}
	}

	recent := 20
	if rawRecent := strings.TrimSpace(q.Get("recent")); rawRecent != "" {
		if v, err := strconv.Atoi(rawRecent); err == nil && v > 0 {
			recent = v
		}
	}

	tz := 0
	if rawTZ := strings.TrimSpace(q.Get("tz")); rawTZ != "" {
		if v, err := strconv.Atoi(rawTZ); err == nil {
			tz = v
		}
	}

	opts := usageledger.QueryOptions{
		RangeKey:          rangeKey,
		StartMs:           startMs,
		EndMs:             endMs,
		Top:               top,
		Recent:            recent,
		TimezoneOffsetMin: tz,
	}

	result, err := h.UsageLedger.Query(opts)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": err.Error()})
		return
	}

	// Always return fresh ETag based on store revision
	curEtag := LedgerETag(result.Revision)
	w.Header().Set("ETag", curEtag)
	if ifNoneMatch != "" && ifNoneMatch == curEtag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) postBackfill(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.UsageLedger == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"detail": "usage ledger is not configured"})
		return
	}

	force := r.URL.Query().Get("force") == "1"
	if err := h.UsageLedger.BackfillFromHistory(h.ChatHistory, force); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}

	result, err := h.UsageLedger.Query(usageledger.QueryOptions{RangeKey: "all"})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"backfill": result.Backfill,
		"totals":   result.Totals,
	})
}
