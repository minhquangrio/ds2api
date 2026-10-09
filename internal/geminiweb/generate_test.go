package geminiweb

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestGenerateErrorClassification(t *testing.T) {
	t.Run("Throttled1095ReturnsErrGeminiThrottled", func(t *testing.T) {
		errorPayload := `[["wrb.fr",null,null,null,null,[8,null,[["type.googleapis.com/assistant.boq.bard.application.BardErrorInfo",[1095]]]]]]`
		raw := buildFrame(errorPayload, true)

		reader := &StreamReader{
			body:   io.NopCloser(strings.NewReader(raw)),
			parser: NewStreamParser(),
		}

		var lastChunk *ParsedChunk
		for {
			chunk, err := reader.ReadChunk()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatalf("unexpected read error: %v", err)
			}
			if chunk != nil {
				lastChunk = chunk
			}
		}

		var finalErr error
		if reader.parser.LastErrorCode() == 1095 && (lastChunk == nil || !lastChunk.IsFinished) {
			finalErr = ErrGeminiThrottled
		} else if lastChunk == nil {
			finalErr = errors.New("empty response from gemini generate")
		}

		if !errors.Is(finalErr, ErrGeminiThrottled) {
			t.Fatalf("expected ErrGeminiThrottled, got: %v", finalErr)
		}
		if !IsThrottledError(finalErr) {
			t.Fatalf("IsThrottledError should be true")
		}
	})

	t.Run("EmptyResponseReturnsOriginalError", func(t *testing.T) {
		reader := &StreamReader{
			body:   io.NopCloser(strings.NewReader("")),
			parser: NewStreamParser(),
		}

		var lastChunk *ParsedChunk
		for {
			chunk, err := reader.ReadChunk()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatalf("unexpected read error: %v", err)
			}
			if chunk != nil {
				lastChunk = chunk
			}
		}

		var finalErr error
		if reader.parser.LastErrorCode() == 1095 && (lastChunk == nil || !lastChunk.IsFinished) {
			finalErr = ErrGeminiThrottled
		} else if lastChunk == nil {
			finalErr = errors.New("empty response from gemini generate")
		}

		if finalErr == nil || finalErr.Error() != "empty response from gemini generate" {
			t.Fatalf("expected 'empty response from gemini generate', got: %v", finalErr)
		}
		if IsThrottledError(finalErr) {
			t.Fatalf("IsThrottledError should be false for non-1095 empty response")
		}
	})
}
