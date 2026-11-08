package images

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/mediacompat"
	"ds2api/internal/muse"
)

const defaultOwnershipTTL = 30 * time.Minute

// ownedMedia records that a specific caller was handed a specific worker media
// id. The worker keeps blobs behind its shared secret and does not know DS2API
// keys, so caller isolation is enforced here.
type ownedMedia struct {
	contentType string
	expiresAt   time.Time
}

type mediaOwnership struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]ownedMedia
}

func NewMediaOwnership(ttl time.Duration) *mediaOwnership {
	if ttl <= 0 {
		ttl = defaultOwnershipTTL
	}
	return &mediaOwnership{ttl: ttl, entries: map[string]ownedMedia{}}
}

func mediaOwnershipKey(callerID, mediaID string) string {
	return callerID + "\x00" + mediaID
}

func (s *mediaOwnership) register(callerID string, result mediacompat.MediaResult) {
	if s == nil || callerID == "" {
		return
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	for _, item := range result.Items {
		if strings.TrimSpace(item.MediaID) == "" {
			continue
		}
		s.entries[mediaOwnershipKey(callerID, item.MediaID)] = ownedMedia{
			contentType: item.ContentType,
			expiresAt:   now.Add(s.ttl),
		}
	}
}

func (s *mediaOwnership) claim(callerID, mediaID string) (string, bool) {
	if s == nil || callerID == "" || mediaID == "" {
		return "", false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[mediaOwnershipKey(callerID, mediaID)]
	if !ok {
		return "", false
	}
	if entry.expiresAt.Before(now) {
		delete(s.entries, mediaOwnershipKey(callerID, mediaID))
		return "", false
	}
	return entry.contentType, true
}

func (s *mediaOwnership) sweepLocked(now time.Time) {
	for key, entry := range s.entries {
		if entry.expiresAt.Before(now) {
			delete(s.entries, key)
		}
	}
}

// MediaBytes serves GET /v1/images/media/{media_id}: the caller's own generated
// image bytes, fetched from the worker and streamed back. Entries are per
// process, so multi-instance deployments should request b64_json instead.
func (h *Handler) MediaBytes(w http.ResponseWriter, r *http.Request) {
	mediaID := strings.TrimSpace(chi.URLParam(r, "media_id"))
	if mediaID == "" {
		shared.WriteOpenAIError(w, http.StatusBadRequest, "media id is required")
		return
	}
	a, err := h.Auth.DetermineForProvider(r, "muse")
	if err != nil {
		status, detail := auth.MapAuthStatus(err)
		shared.WriteOpenAIError(w, status, detail)
		return
	}
	defer h.Auth.Release(a)

	if _, ok := h.Ownership.claim(a.CallerID, mediaID); !ok {
		shared.WriteOpenAIErrorWithCode(w, http.StatusNotFound, "media not found for this caller", "not_found")
		return
	}
	data, contentType, err := muse.FetchMediaBytes(r.Context(), a, h.Store, mediaID)
	if err != nil {
		status, code := muse.MediaErrorDetail(err)
		shared.WriteOpenAIErrorWithCode(w, status, err.Error(), code)
		return
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	if _, writeErr := w.Write(data); writeErr != nil {
		return
	}
}
