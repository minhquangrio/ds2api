package geminiweb

import (
	"fmt"
	"strings"
	"testing"
)

// utf16Units counts a string the way Google's frame length marker does:
// JavaScript string length, so astral characters cost two units.
func utf16Units(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func buildFrame(payload string, withPrefix bool) string {
	prefix := ""
	if withPrefix {
		prefix = ")]}'\n"
	}
	framed := "\n" + payload + "\n"
	return fmt.Sprintf("%s%d%s", prefix, utf16Units(framed), framed)
}

func TestStreamParserFeedAndDeltas(t *testing.T) {
	parser := NewStreamParser()

	payload1 := `[["wrb.fr",null,"[null,[\"cid_123\",\"rid_456\"],null,null,[[\"rcid_789\",[\"Hello\"],null,null,null,null,null,null,[1],null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,[[\"Thinking process...\"]]]]]"]]`

	chunks1, err := parser.Feed([]byte(buildFrame(payload1, true)))
	if err != nil {
		t.Fatalf("Feed frame 1 failed: %v", err)
	}
	if len(chunks1) != 1 {
		t.Fatalf("expected 1 chunk from frame 1, got %d", len(chunks1))
	}
	if chunks1[0].TextDelta != "Hello" {
		t.Errorf("expected TextDelta 'Hello', got %q", chunks1[0].TextDelta)
	}
	if chunks1[0].ThoughtDelta != "Thinking process..." {
		t.Errorf("expected ThoughtDelta 'Thinking process...', got %q", chunks1[0].ThoughtDelta)
	}
	if chunks1[0].IsFinished {
		t.Errorf("frame 1 should not be finished")
	}

	// Frame 2: accumulated text with the completion indicator set.
	payload2 := `[["wrb.fr",null,"[null,[\"cid_123\",\"rid_456\"],null,null,[[\"rcid_789\",[\"Hello world!\"],null,null,null,null,null,null,[2],null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,null,[[\"Thinking process...\"]]]]]"]]`

	chunks2, err := parser.Feed([]byte(buildFrame(payload2, false)))
	if err != nil {
		t.Fatalf("Feed frame 2 failed: %v", err)
	}
	if len(chunks2) != 1 {
		t.Fatalf("expected 1 chunk from frame 2, got %d", len(chunks2))
	}
	if chunks2[0].TextDelta != " world!" {
		t.Errorf("expected TextDelta ' world!', got %q", chunks2[0].TextDelta)
	}
	if chunks2[0].ThoughtDelta != "" {
		t.Errorf("expected empty ThoughtDelta since thoughts didn't change, got %q", chunks2[0].ThoughtDelta)
	}
	if !chunks2[0].IsFinished {
		t.Errorf("frame 2 should be finished (indicator=2)")
	}
}

// TestStreamParserNonASCII guards the frame length accounting. The marker counts
// UTF-16 code units, so reading it as a byte count truncates every payload with
// accented letters, CJK or emoji and desynchronises the frames that follow.
func TestStreamParserNonASCII(t *testing.T) {
	parser := NewStreamParser()

	// "Xin chào! 你好 🎉" mixes BMP accented letters, CJK and an astral emoji:
	// 14 code units but 26 UTF-8 bytes.
	text := "Xin chào! 你好 🎉"
	if units, byteLen := utf16Units(text), len(text); units == byteLen {
		t.Fatalf("fixture is not exercising UTF-16 width: units=%d bytes=%d", units, byteLen)
	}

	first := `[["wrb.fr",null,"[null,[\"c1\",\"r1\"],null,null,[[\"rc1\",[\"` + text + `\"]]]]"]]`
	chunks, err := parser.Feed([]byte(buildFrame(first, true)))
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk carrying non-ASCII text, got %d", len(chunks))
	}
	if chunks[0].TextDelta != text {
		t.Errorf("text corrupted by frame split:\n got %q\nwant %q", chunks[0].TextDelta, text)
	}

	// The frame after a non-ASCII one must still line up.
	second := `[["wrb.fr",null,"[null,[\"c1\",\"r1\"],null,null,[[\"rc1\",[\"` + text + ` tail\"]]]]"]]`
	chunks2, err := parser.Feed([]byte(buildFrame(second, false)))
	if err != nil {
		t.Fatalf("Feed frame 2 failed: %v", err)
	}
	if len(chunks2) != 1 {
		t.Fatalf("expected 1 chunk from frame 2, got %d", len(chunks2))
	}
	if chunks2[0].TextDelta != " tail" {
		t.Errorf("frame 2 misaligned after non-ASCII frame: got %q", chunks2[0].TextDelta)
	}
}

// TestGrowthHandlesMidStringRewrite covers the case a plain prefix check misses:
// Gemini escaping a Markdown marker rewrites the tail, and the stable head must
// still be recognised rather than the whole frame being dropped.
func TestGrowthHandlesMidStringRewrite(t *testing.T) {
	if got := growth("Hello world!", "Hello"); got != " world!" {
		t.Errorf("prefix growth: got %q", got)
	}
	// A backslash appears before the emphasis marker mid-stream.
	if got := growth("a \\*b\\* end", "a *b"); got != "\\* end" {
		t.Errorf("mid-string rewrite: got %q", got)
	}
	if got := growth("anything", ""); got != "anything" {
		t.Errorf("first emission: got %q", got)
	}
	if got := growth("totally different", "no overlap here"); got != "" {
		t.Errorf("no shared prefix should emit nothing, got %q", got)
	}
}

func TestCleanGeminiText(t *testing.T) {
	if got := cleanGeminiText("code\n```"); got != "code" {
		t.Errorf("trailing fence should be trimmed, got %q", got)
	}
	if got := cleanGeminiText(""); got != "" {
		t.Errorf("empty stays empty, got %q", got)
	}
	if !strings.Contains(cleanGeminiText("plain text"), "plain text") {
		t.Errorf("plain text must survive cleaning")
	}
}

func TestStreamParserNoCandidateBlockReason(t *testing.T) {
	parser := NewStreamParser()

	payload := `[["wrb.fr",null,"[null,[\"cid_123\",\"rid_456\"],null,null,null]",null,null,null,"BLOCKED_SAFETY"]]`
	chunks, err := parser.Feed([]byte(buildFrame(payload, true)))
	if err != nil {
		t.Fatalf("Feed failed: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks from no-candidate frame, got %d", len(chunks))
	}
	reason := parser.BlockReason()
	if reason == "" {
		t.Errorf("expected non-empty BlockReason for no-candidate frame")
	}
	if !strings.Contains(reason, "BLOCKED_SAFETY") {
		t.Errorf("expected BlockReason to contain metadata 'BLOCKED_SAFETY', got %q", reason)
	}
}
