package codex

import (
	"bytes"
	"encoding/json"
	"strings"
)

type Event struct {
	Type string `json:"type"`
	Data string `json:"data"`
}

type StreamParser struct {
	buffer []byte
}

func NewStreamParser() *StreamParser {
	return &StreamParser{
		buffer: make([]byte, 0, 4096),
	}
}

func (p *StreamParser) Feed(chunk []byte) []Event {
	p.buffer = append(p.buffer, chunk...)
	var events []Event

	for {
		idx := bytes.Index(p.buffer, []byte("\n\n"))
		var delimLen = 2
		if idx == -1 {
			idx = bytes.Index(p.buffer, []byte("\r\n\r\n"))
			delimLen = 4
		}
		if idx == -1 {
			break
		}

		block := p.buffer[:idx]
		p.buffer = p.buffer[idx+delimLen:]

		ev := parseEventBlock(block)
		if ev.Data != "" || ev.Type != "" {
			events = append(events, ev)
		}
	}

	return events
}

func parseEventBlock(block []byte) Event {
	lines := strings.Split(string(block), "\n")
	var eventType string
	var dataLines []string

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "event:") {
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
		}
	}

	dataStr := strings.TrimSpace(strings.Join(dataLines, "\n"))
	return Event{
		Type: eventType,
		Data: dataStr,
	}
}

type ResponsesOutputItem struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (e Event) ParseTextDelta() (string, bool) {
	if e.Type == "response.text.delta" || e.Type == "response.output_text.delta" {
		var obj struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(e.Data), &obj); err == nil && obj.Delta != "" {
			return obj.Delta, true
		}
	}
	return "", false
}

func (e Event) ParseReasoningDelta() (string, bool) {
	if e.Type == "response.reasoning.delta" || e.Type == "response.reasoning_text.delta" {
		var obj struct {
			Delta string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(e.Data), &obj); err == nil && obj.Delta != "" {
			return obj.Delta, true
		}
	}
	return "", false
}

func (e Event) ParseFunctionCallArgumentsDelta() (callID, delta string, ok bool) {
	if e.Type == "response.function_call_arguments.delta" {
		var obj struct {
			CallID string `json:"call_id"`
			Delta  string `json:"delta"`
		}
		if err := json.Unmarshal([]byte(e.Data), &obj); err == nil {
			return obj.CallID, obj.Delta, true
		}
	}
	return "", "", false
}

func (e Event) ParseCompletedUsage() (inputTokens, outputTokens, totalTokens int, ok bool) {
	term, isTerminal := e.ParseTerminal()
	if !isTerminal || term.Failed {
		return 0, 0, 0, false
	}
	return term.InputTokens, term.OutputTokens, term.TotalTokens, true
}

// StreamTerminal describes how the upstream stream ended. Callers must use it
// instead of assuming success: `response.incomplete` means the output was cut
// short and `response.failed` means the turn errored after events had already
// been forwarded to the client.
type StreamTerminal struct {
	Type         string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Failed       bool
	ErrorMessage string
}

func (e Event) ParseTerminal() (StreamTerminal, bool) {
	switch e.Type {
	case "response.completed", "response.done", "response.incomplete", "response.failed":
	default:
		if strings.TrimSpace(e.Data) == "[DONE]" {
			return StreamTerminal{Type: "[DONE]"}, true
		}
		return StreamTerminal{}, false
	}

	var obj struct {
		Response struct {
			Usage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
				TotalTokens  int `json:"total_tokens"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		} `json:"response"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(e.Data), &obj)

	term := StreamTerminal{
		Type:         e.Type,
		InputTokens:  obj.Response.Usage.InputTokens,
		OutputTokens: obj.Response.Usage.OutputTokens,
		TotalTokens:  obj.Response.Usage.TotalTokens,
	}
	if term.TotalTokens == 0 && (term.InputTokens > 0 || term.OutputTokens > 0) {
		term.TotalTokens = term.InputTokens + term.OutputTokens
	}
	if e.Type == "response.failed" {
		term.Failed = true
		if obj.Response.Error != nil {
			term.ErrorMessage = obj.Response.Error.Message
		} else if obj.Error != nil {
			term.ErrorMessage = obj.Error.Message
		}
		if term.ErrorMessage == "" {
			term.ErrorMessage = "upstream reported response.failed"
		}
	}
	return term, true
}

// ToolCallTracker assigns a stable, dense index to each tool call id. Upstream
// may interleave parallel calls, so the index cannot be derived from arrival
// order of argument deltas alone and must not be hard-coded to zero.
type ToolCallTracker struct {
	order []string
	index map[string]int
	names map[string]string
	args  map[string]string
}

func NewToolCallTracker() *ToolCallTracker {
	return &ToolCallTracker{
		index: make(map[string]int),
		names: make(map[string]string),
		args:  make(map[string]string),
	}
}

func (t *ToolCallTracker) IndexFor(callID string) int {
	if i, ok := t.index[callID]; ok {
		return i
	}
	i := len(t.order)
	t.order = append(t.order, callID)
	t.index[callID] = i
	return i
}

func (t *ToolCallTracker) SetName(callID, name string) {
	if name != "" {
		t.names[callID] = name
	}
}

func (t *ToolCallTracker) Name(callID string) string { return t.names[callID] }

func (t *ToolCallTracker) AppendArgs(callID, delta string) {
	t.args[callID] += delta
}

func (t *ToolCallTracker) Args(callID string) string { return t.args[callID] }

func (t *ToolCallTracker) Order() []string { return t.order }

func (t *ToolCallTracker) Len() int { return len(t.order) }
