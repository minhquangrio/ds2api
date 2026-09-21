package geminiweb

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Google's XSSI prefix, prepended once to a response body.
const xssiPrefix = ")]}'"

type frameStatus int

const (
	frameNeedMore frameStatus = iota
	frameFound
	frameInvalid
)

// scanFrame reads one length-prefixed frame off the front of buf.
//
// The frame format is "<length>\n<payload>", where <length> counts UTF-16 code
// units - JavaScript string length semantics - and not bytes. Any character
// outside the BMP (an emoji, for instance) therefore costs two units while
// occupying four UTF-8 bytes, so the marker cannot be used as a byte count.
// Counting units and converting to a byte offset is the only correct reading;
// treating the marker as a byte length silently truncates every non-ASCII
// payload and desynchronises the rest of the stream.
//
// This mirrors StreamingFrameParser in gemini-webapi's utils/parsing.py.
//
// Returns the payload (without the length line), the unconsumed remainder, and
// whether a complete frame was available. On frameInvalid nothing is consumed,
// so rest is the buffer unchanged and the caller can resynchronise.
func scanFrame(buf string) (payload, rest string, status frameStatus) {
	i := 0
	for i < len(buf) && isSpaceByte(buf[i]) {
		i++
	}
	if i > 0 {
		buf = buf[i:]
	}
	if buf == "" {
		return "", buf, frameNeedMore
	}

	// Length marker: digits terminated by a newline.
	j := 0
	for j < len(buf) && buf[j] >= '0' && buf[j] <= '9' {
		j++
	}
	if j == 0 {
		return "", buf, frameInvalid
	}
	if j >= len(buf) {
		// Digits may still be arriving.
		return "", buf, frameNeedMore
	}
	if buf[j] != '\n' {
		return "", buf, frameInvalid
	}

	units, err := strconv.Atoi(buf[:j])
	if err != nil {
		return "", buf, frameInvalid
	}

	// Walk the payload counting UTF-16 units and bytes until the marker is met.
	// In Google's protocol (matching StreamingFrameParser in gemini-webapi),
	// the unit count includes the delimiter newline following the digits.
	start := j
	off := start
	seen := 0
	for seen < units && off < len(buf) {
		r, size := utf8.DecodeRuneInString(buf[off:])
		if r == utf8.RuneError && size <= 1 {
			// Truncated multi-byte rune at the buffer edge; wait for more.
			if off+size >= len(buf) {
				return "", buf, frameNeedMore
			}
			// Invalid byte mid-stream: treat it as one unit and move on.
		}
		u := 1
		if r > 0xFFFF {
			u = 2
		}
		if seen+u > units {
			break
		}
		seen += u
		off += size
	}
	if seen < units {
		return "", buf, frameNeedMore
	}

	return strings.TrimSpace(buf[start:off]), buf[off:], frameFound
}

// resyncOffset returns the offset of the next JSON frame opening ("[[") in buf,
// or -1 when there is none.
//
// Used to recover after a malformed length marker. Batchexecute responses are
// not always length-prefixed - some arrive as plain NDJSON - so treating an
// unreadable marker as fatal would discard a whole usable response.
func resyncOffset(buf string) int {
	return strings.Index(buf, "[[")
}

// stripXSSIPrefix removes Google's ")]}'" prefix and any separator newline that
// follows it. Callers feed a partial buffer, so an incomplete prefix is left
// untouched and reported as "not yet stripped".
func stripXSSIPrefix(buf string) (string, bool) {
	if !strings.HasPrefix(buf, xssiPrefix) {
		if strings.HasPrefix(xssiPrefix, buf) {
			// Still receiving the prefix itself.
			return buf, false
		}
		return buf, true
	}
	return strings.TrimLeft(buf[len(xssiPrefix):], "\r\n"), true
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
