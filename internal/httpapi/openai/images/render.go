package images

import (
	"strings"
	"time"

	"ds2api/internal/mediacompat"
)

// renderImagesResponse builds the OpenAI-shaped images body. `created` and
// `data` follow the upstream contract; `model`, `usage`, `thread_url` and the
// per-item content_type/width/height are additive fields DS2API documents for
// clients that need to know what produced the media.
func renderImagesResponse(req mediacompat.ImageRequest, result mediacompat.MediaResult, base string) map[string]any {
	created := result.Created
	if created == 0 {
		created = time.Now().Unix()
	}
	data := make([]map[string]any, 0, len(result.Items))
	for _, item := range result.Items {
		entry := map[string]any{"content_type": item.ContentType}
		if item.Width > 0 {
			entry["width"] = item.Width
		}
		if item.Height > 0 {
			entry["height"] = item.Height
		}
		switch {
		case item.B64JSON != "":
			entry["b64_json"] = item.B64JSON
		case item.MediaID != "" && base != "":
			entry["url"] = absoluteMediaURL(base, item)
		case item.URL != "" && base != "":
			entry["url"] = absoluteMediaURL(base, item)
		default:
			// A media item without bytes and without a fetchable id is a worker
			// contract violation; report it as an empty slot rather than lying.
			entry["b64_json"] = ""
		}
		data = append(data, entry)
	}
	model := result.Model
	if model == "" {
		model = req.ResponseModel
	}
	body := map[string]any{"created": created, "data": data, "model": model}
	if len(result.Usage) > 0 {
		body["usage"] = result.Usage
	}
	if result.ThreadURL != "" {
		body["thread_url"] = result.ThreadURL
	}
	return body
}

// absoluteMediaURL points url responses at this gateway, never at a worker
// hostname or an internal file path.
func absoluteMediaURL(base string, item mediacompat.MediaItem) string {
	trimmed := strings.TrimRight(base, "/")
	if item.MediaID != "" {
		return trimmed + "/v1/images/media/" + item.MediaID
	}
	if strings.HasPrefix(item.URL, "https://") || strings.HasPrefix(item.URL, "http://") {
		return item.URL
	}
	return trimmed
}
