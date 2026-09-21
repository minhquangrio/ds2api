package geminiweb

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"ds2api/internal/config"
)

type ParsedChunk struct {
	TextDelta    string
	ThoughtDelta string
	FullText     string
	FullThought  string
	IsFinished   bool
	CID          string
	RID          string
	RCID         string
}

type StreamParser struct {
	buffer      string
	prefixDone  bool
	lastText    string
	lastThought string
	CID         string
	RID         string
	RCID        string
}

func NewStreamParser() *StreamParser {
	return &StreamParser{}
}

// Feed receives raw bytes from the HTTP stream and returns completed chunks.
//
// Frames are length-prefixed with a UTF-16 code-unit count, so splitting is
// delegated to scanFrame; reading the marker as a byte count truncates any
// payload carrying non-ASCII text and desynchronises the rest of the stream.
func (p *StreamParser) Feed(data []byte) ([]ParsedChunk, error) {
	if len(data) == 0 {
		return nil, nil
	}
	p.buffer += string(data)

	if !p.prefixDone {
		stripped, done := stripXSSIPrefix(p.buffer)
		p.buffer = stripped
		p.prefixDone = done
		if !done {
			return nil, nil
		}
	}

	var results []ParsedChunk
	for {
		payload, rest, status := scanFrame(p.buffer)
		if status == frameNeedMore {
			// Partial frame stays buffered for the next read.
			break
		}
		if status == frameInvalid {
			// No readable length marker: resynchronise on the next frame opening
			// rather than discarding everything still buffered.
			off := resyncOffset(p.buffer)
			if off <= 0 {
				if len(p.buffer) == 0 {
					break
				}
				p.buffer = p.buffer[1:] // guarantee progress
				continue
			}
			config.Logger.Warn("[geminiweb] skipping malformed stream frame")
			p.buffer = p.buffer[off:]
			continue
		}
		p.buffer = rest
		if strings.TrimSpace(payload) == "" {
			continue
		}
		chunk, err := p.parseFrame(payload)
		if err != nil {
			config.Logger.Warn("[geminiweb] skipping stream frame", "error", err)
			continue
		}
		if chunk != nil {
			results = append(results, *chunk)
		}
	}

	return results, nil
}

func (p *StreamParser) parseFrame(payload string) (*ParsedChunk, error) {
	var envelopes [][]any
	if err := json.Unmarshal([]byte(payload), &envelopes); err != nil || len(envelopes) == 0 {
		return nil, err
	}

	for _, env := range envelopes {
		if len(env) < 3 {
			continue
		}
		innerStr, ok := env[2].(string)
		if !ok || innerStr == "" {
			continue
		}

		var inner []any
		if err := json.Unmarshal([]byte(innerStr), &inner); err != nil {
			continue
		}

		// Conversation and reply ids live in inner[1].
		if meta, ok := nestedValue(inner, 1).([]any); ok {
			if c, ok := nestedValue(meta, 0).(string); ok && c != "" {
				p.CID = c
			}
			if r, ok := nestedValue(meta, 1).(string); ok && r != "" {
				p.RID = r
			}
		}

		candidate, ok := nestedValue(inner, 4, 0).([]any)
		if !ok {
			continue
		}
		if rcid, ok := nestedValue(candidate, 0).(string); ok && rcid != "" {
			p.RCID = rcid
		}

		fullText, _ := nestedValue(candidate, 1, 0).(string)
		// A card-content placeholder stands in for the real text one slot over.
		if cardContentRe.MatchString(fullText) {
			if alt, ok := nestedValue(candidate, 22, 0).(string); ok && alt != "" {
				fullText = alt
			}
		}
		fullText = cleanGeminiArtifacts(fullText)

		fullThought, _ := nestedValue(candidate, 37, 0, 0).(string)

		// indicator == 2 means this turn is complete (client.py: is_completed).
		isFinished := false
		if v, ok := toInt(nestedValue(candidate, 8, 0)); ok && v == 2 {
			isFinished = true
		}

		textDelta := ""
		if isFinished {
			// The finished frame carries the settled text; emit the remainder as-is.
			if fullText != p.lastText {
				textDelta = growth(fullText, p.lastText)
				p.lastText = fullText
			}
		} else if newClean := cleanGeminiText(fullText); newClean != p.lastText {
			textDelta = growth(newClean, p.lastText)
			p.lastText = newClean
		}

		thoughtDelta := ""
		if fullThought != p.lastThought {
			thoughtDelta = growth(fullThought, p.lastThought)
			p.lastThought = fullThought
		}

		if textDelta == "" && thoughtDelta == "" && !isFinished {
			continue
		}

		return &ParsedChunk{
			TextDelta:    textDelta,
			ThoughtDelta: thoughtDelta,
			FullText:     fullText,
			FullThought:  fullThought,
			IsFinished:   isFinished,
			CID:          p.CID,
			RID:          p.RID,
			RCID:         p.RCID,
		}, nil
	}

	return nil, nil
}

// growth returns the part of next not yet emitted.
//
// gemini-webapi uses difflib.SequenceMatcher here (get_delta_by_fp_len) because
// Gemini rewrites the tail of a message as it streams - escaping a Markdown
// marker, closing a code fence. A plain HasPrefix test emits nothing at all when
// that happens, silently dropping text. This walks the common prefix to find what
// is stable, then skips past the already-sent remainder if it reappears slightly
// later, so a rewrite is emitted once rather than duplicated.
//
// Returns "" when the two share no prefix: emitting the whole string would
// duplicate what the client already has.
func growth(next, sent string) string {
	if sent == "" {
		return next
	}

	n := 0
	for n < len(next) && n < len(sent) && next[n] == sent[n] {
		n++
	}
	// Never split a multi-byte rune: distinct runes can share leading bytes.
	for n > 0 && !utf8.RuneStart(next[n]) {
		n--
	}
	if n == 0 {
		return ""
	}

	if tail := sent[n:]; tail != "" {
		if idx := strings.Index(next[n:], tail); idx >= 0 {
			return next[n+idx+len(tail):]
		}
	}
	return next[n:]
}

var (
	artifactsRe   = regexp.MustCompile(`https?://googleusercontent\.com/(?:\w+/)+\d+\n*`)
	cardContentRe = regexp.MustCompile(`^https?://googleusercontent\.com/card_content/\d+`)
	// Trailing escapes on a Markdown marker, e.g. "\*" mid-stream.
	flickerEscRe = regexp.MustCompile("\\\\+[`*_~].*$")
)

func cleanGeminiArtifacts(s string) string {
	return artifactsRe.ReplaceAllString(s, "")
}

// cleanGeminiText drops the transient artifacts Gemini leaves while streaming,
// mirroring get_clean_text() in gemini-webapi's utils/parsing.py.
func cleanGeminiText(s string) string {
	if s == "" {
		return ""
	}
	s = strings.TrimSuffix(s, "\n```")
	return flickerEscRe.ReplaceAllString(s, "")
}
