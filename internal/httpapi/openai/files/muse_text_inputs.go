package files

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/httpapi/openai/shared"
)

func isMuseFileRequest(h *Handler, a *auth.RequestAuth, req map[string]any) bool {
	if a != nil && (a.Provider == "muse" || a.Account.IsMuse()) {
		return true
	}
	model, _ := req["model"].(string)
	target, ok := config.ResolveModelTarget(h.Store, model)
	return ok && target.Provider == "muse"
}

// Muse chat accepts local text context, never DeepSeek file references.
func (h *Handler) preprocessMuseTextInputs(ctx context.Context, a *auth.RequestAuth, req map[string]any) error {
	maxBytes := 3 * 1024 * 1024
	var extensions map[string]struct{}
	if h.Store != nil {
		maxBytes = h.Store.ExpertTextFileInlineMaxFileBytes()
		extensions = h.Store.ExpertTextFileInlineAllowedExtensions()
	}
	state := &expertTextInlineState{ctx: ctx, handler: h, auth: a, store: h.ContentStore, maxFileBytes: maxBytes, allowedExts: extensions}
	unsupported := func() error {
		return &inlineFileUploadError{status: http.StatusBadRequest, message: "Muse chat supports text files only; this attachment cannot be read."}
	}
	var walk func(any) (any, error)
	walk = func(value any) (any, error) {
		switch v := value.(type) {
		case []any:
			out := make([]any, len(v))
			for i, item := range v {
				updated, err := walk(item)
				if err != nil {
					return nil, err
				}
				out[i] = updated
			}
			return out, nil
		case map[string]any:
			if replacement, ok, err := state.tryInlineTextBlock(v); ok || err != nil {
				return replacement, err
			}
			typ := strings.ToLower(shared.AsString(v["type"]))
			for _, kind := range []string{"file", "input_file", "image", "image_url", "input_image", "audio", "input_audio", "video", "video_url"} {
				if typ == kind {
					return nil, unsupported()
				}
			}
			if shared.AsString(v["file_id"]) != "" {
				return nil, unsupported()
			}
			for _, key := range []string{"messages", "input", "attachments", "content", "files", "items", "data", "source", "file", "image_url"} {
				if nested, ok := v[key]; ok {
					updated, err := walk(nested)
					if err != nil {
						return nil, err
					}
					v[key] = updated
				}
			}
			return v, nil
		default:
			return value, nil
		}
	}
	for _, key := range []string{"messages", "input", "attachments"} {
		if value, ok := req[key]; ok {
			updated, err := walk(value)
			if err != nil {
				return err
			}
			req[key] = updated
		}
	}
	for _, id := range collectTopLevelFileIDs(req) {
		block, ok, err := state.inlineStoredFile(id)
		if err != nil {
			return err
		}
		if !ok {
			return unsupported()
		}
		text := shared.AsString(block["text"])
		if messages, ok := req["messages"].([]any); ok && lastUserMessageIndex(req) >= 0 {
			msg := messages[lastUserMessageIndex(req)].(map[string]any)
			msg["content"] = append(normalizeContentBlocks(msg["content"]), block)
		} else if input, ok := req["input"]; ok {
			switch value := input.(type) {
			case string:
				req["input"] = value + "\n\n" + text
			case []any:
				req["input"] = append(value, map[string]any{"role": "user", "content": []any{block}})
			default:
				return unsupported()
			}
		} else {
			return &inlineFileUploadError{status: http.StatusBadRequest, message: fmt.Sprintf("no text input for file %q", id)}
		}
	}
	delete(req, "file_ids")
	delete(req, "ref_file_ids")
	return nil
}
