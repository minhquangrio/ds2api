package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"ds2api/internal/assistantturn"
	"ds2api/internal/config"
	"ds2api/internal/geminiweb"
	"ds2api/internal/promptcompat"
	"ds2api/internal/toolcall"
)

type StoreSnapshotter interface {
	Snapshot() config.Config
}

type Runtime struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

var defaultRuntime = &Runtime{
	clients: make(map[string]*Client),
}

func DefaultRuntime() *Runtime {
	return defaultRuntime
}

func (r *Runtime) InvalidateClient(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "default"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.clients, id)
}

func (r *Runtime) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients = make(map[string]*Client)
}

func (r *Runtime) GetClient(ctx context.Context, acc config.Account, store any) (*Client, error) {
	token := strings.TrimSpace(acc.Token)
	if token == "" {
		return nil, errors.New("codex account has no access token (requires login/refresh)")
	}

	id := acc.Identifier()
	if id == "" {
		id = "default"
	}

	proxyURL := ""
	defaultModel := ""
	if snap, ok := store.(StoreSnapshotter); ok {
		cfg := snap.Snapshot()
		defaultModel = strings.TrimSpace(cfg.Codex.DefaultModel)
		if proxyID := strings.TrimSpace(acc.ProxyID); proxyID != "" {
			for _, p := range cfg.Proxies {
				if p.ID == proxyID {
					proxyURL = geminiweb.FormatProxyURL(p)
					break
				}
			}
		}
	}

	r.mu.RLock()
	existing, found := r.clients[id]
	r.mu.RUnlock()
	if found {
		existing.SetToken(token)
		existing.SetDefaultModel(defaultModel)
		return existing, nil
	}

	client := NewClient(token, acc.CodexAccountID, proxyURL)
	client.SetDefaultModel(defaultModel)

	r.mu.Lock()
	r.clients[id] = client
	r.mu.Unlock()

	return client, nil
}

func ExecuteTurn(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest) (assistantturn.Turn, error) {
	body, err := BuildResponsesBodyWithDefault(stdReq, client.DefaultModel())
	if err != nil {
		return assistantturn.Turn{}, err
	}

	payloadBytes, err := json.Marshal(body)
	if err != nil {
		return assistantturn.Turn{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, CodexResponsesURL, bytes.NewReader(payloadBytes))
	if err != nil {
		return assistantturn.Turn{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.DoStream(req)
	if err != nil {
		return assistantturn.Turn{}, fmt.Errorf("codex request failed: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			log.Printf("codex: failed to close turn response body: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return assistantturn.Turn{}, fmt.Errorf("codex upstream error (%d): %s", resp.StatusCode, string(b))
	}

	parser := NewStreamParser()
	var textBuf, reasoningBuf strings.Builder
	var toolCalls []toolcall.ParsedToolCall
	tools := NewToolCallTracker()
	emitted := make(map[string]bool)

	appendToolCall := func(callID, name, arguments string) {
		if callID == "" || emitted[callID] {
			return
		}
		emitted[callID] = true
		tc := toolcall.ParsedToolCall{
			Name:  name,
			Input: make(map[string]any),
		}
		if strings.TrimSpace(arguments) != "" {
			_ = json.Unmarshal([]byte(arguments), &tc.Input)
		}
		toolCalls = append(toolCalls, tc)
	}

	var usage assistantturn.Usage
	var termErr error

	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			events := parser.Feed(buf[:n])
			for _, ev := range events {
				if txt, ok := ev.ParseTextDelta(); ok {
					textBuf.WriteString(txt)
				}
				if rsn, ok := ev.ParseReasoningDelta(); ok {
					reasoningBuf.WriteString(rsn)
				}
				if term, ok := ev.ParseTerminal(); ok {
					if term.Failed {
						termErr = fmt.Errorf("codex upstream ended with response.failed: %s", term.ErrorMessage)
						continue
					}
					// Usage is only reported by the upstream on the terminal
					// event, so it is left zero when absent instead of being
					// estimated from string lengths.
					usage = assistantturn.Usage{
						InputTokens:  term.InputTokens,
						OutputTokens: term.OutputTokens,
						TotalTokens:  term.TotalTokens,
					}
				}

				if ev.Type == "response.output_item.added" || ev.Type == "response.output_item.done" {
					var itemWrap struct {
						Item ResponsesOutputItem `json:"item"`
					}
					if err := json.Unmarshal([]byte(ev.Data), &itemWrap); err == nil && itemWrap.Item.Type == "function_call" {
						callID := itemWrap.Item.CallID
						if callID == "" {
							callID = itemWrap.Item.ID
						}
						tools.IndexFor(callID)
						tools.SetName(callID, itemWrap.Item.Name)
						if ev.Type == "response.output_item.done" {
							if itemWrap.Item.Arguments != "" && tools.Args(callID) == "" {
								tools.AppendArgs(callID, itemWrap.Item.Arguments)
							}
							appendToolCall(callID, itemWrap.Item.Name, tools.Args(callID))
						}
					}
				}

				if callID, delta, ok := ev.ParseFunctionCallArgumentsDelta(); ok {
					tools.IndexFor(callID)
					tools.AppendArgs(callID, delta)
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			return assistantturn.Turn{}, rerr
		}
	}

	// Some upstream turns only announce a function call without a done item.
	for _, callID := range tools.Order() {
		appendToolCall(callID, tools.Name(callID), tools.Args(callID))
	}

	if termErr != nil {
		return assistantturn.Turn{}, termErr
	}

	text := textBuf.String()
	reasoning := reasoningBuf.String()

	stopReason := assistantturn.StopReasonStop
	if len(toolCalls) > 0 {
		stopReason = assistantturn.StopReasonToolCalls
	}

	return assistantturn.Turn{
		Model:       stdReq.ResponseModel,
		Prompt:      stdReq.FinalPrompt,
		Text:        text,
		RawText:     text,
		Thinking:    reasoning,
		RawThinking: reasoning,
		ToolCalls:   toolCalls,
		StopReason:  stopReason,
		Usage:       usage,
	}, nil
}
