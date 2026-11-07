package muse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type streamWriter struct {
	w       http.ResponseWriter
	started bool
	err     error
}

func (s *streamWriter) send(event string, data any) {
	if s.err != nil {
		return
	}
	b, err := json.Marshal(data)
	if err != nil {
		s.err = err
		return
	}
	if !s.started {
		setSSEHeaders(s.w)
		s.started = true
	}
	if event != "" {
		_, s.err = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b)
	} else {
		_, s.err = fmt.Fprintf(s.w, "data: %s\n\n", b)
	}
	if s.err == nil {
		if f, ok := s.w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

func (s *streamWriter) done() error {
	if s.err == nil {
		_, s.err = fmt.Fprint(s.w, "data: [DONE]\n\n")
	}
	if s.err == nil {
		if f, ok := s.w.(http.Flusher); ok {
			f.Flush()
		}
	}
	return s.err
}
