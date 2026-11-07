package files

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"ds2api/internal/auth"
)

func TestMuseTextInputsNeverNeedDeepSeek(t *testing.T) {
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: &mapContentStore{data: map[string]*storedContent{
		"fixture": {filename: "context.txt", mimeType: "text/plain", data: []byte("stored text")},
	}}}
	a := &auth.RequestAuth{Provider: "muse"}
	req := map[string]any{"model": "muse-spark", "messages": []any{map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "input_file", "filename": "inline.txt", "file_data": "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("inline text"))},
	}}}, "ref_file_ids": []any{"fixture"}}
	if err := h.PreprocessInlineFileInputs(context.Background(), a, req); err != nil {
		t.Fatal(err)
	}
	msg := req["messages"].([]any)[0].(map[string]any)
	blocks := msg["content"].([]any)
	if len(blocks) != 2 || !strings.Contains(blocks[0].(map[string]any)["text"].(string), "inline text") || blocks[1].(map[string]any)["text"] != "stored text" {
		t.Fatalf("text lost: %#v", req)
	}
	if _, ok := req["ref_file_ids"]; ok {
		t.Fatal("DeepSeek refs retained")
	}
}

func TestMuseRejectsUnreadableAttachment(t *testing.T) {
	h := &Handler{Store: expertInlineMockStore{modelAliases: map[string]string{"alias": "muse-spark"}}}
	for _, block := range []map[string]any{
		{"type": "image_url", "image_url": map[string]any{"url": "https://example.test/image.png"}},
		{"type": "input_file", "file_id": "unknown"},
	} {
		req := map[string]any{"model": "alias", "messages": []any{map[string]any{"role": "user", "content": []any{block}}}}
		if err := h.PreprocessInlineFileInputs(context.Background(), nil, req); err == nil {
			t.Fatal("unreadable attachment ignored")
		}
	}
}
