package geminiweb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	httpcloak "github.com/sardanioss/httpcloak/client"
)

// DiscoverModels runs the otAQ7b (GET_USER_STATUS) batchexecute RPC and caches
// the models Google reports for this account.
//
// This is the only correct source for model ids, tier capacity and capacity
// field: all three vary per account and drift as Google renumbers models, so a
// static table cannot stand in for it. Without a successful discovery the client
// falls back to the free-tier defaults in model_spec.go.
func (c *Client) DiscoverModels(ctx context.Context) (map[string]ModelSpec, error) {
	session := c.Session()
	if session.AccessToken == "" {
		return nil, errors.New("gemini session not initialized")
	}

	params := url.Values{
		"rpcids":      {RPCGetUserStatus},
		"hl":          {session.Language},
		"_reqid":      {"100000"},
		"rt":          {"c"},
		"source-path": {"/app"},
		"bl":          {session.BuildLabel},
	}
	if session.SessionID != "" {
		params.Set("f.sid", session.SessionID)
	}

	headers := c.BuildDefaultHeaders()
	headers["Content-Type"] = []string{"application/x-www-form-urlencoded;charset=utf-8"}

	batchPayload := `[[["` + RPCGetUserStatus + `","[]",null,"generic"]]]`
	postForm := url.Values{
		"at":    {session.AccessToken},
		"f.req": {batchPayload},
	}

	req := &httpcloak.Request{
		Method:  http.MethodPost,
		URL:     BatchExecuteURL + "?" + params.Encode(),
		Headers: headers,
		Body:    strings.NewReader(postForm.Encode()),
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("discover models RPC failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discover models RPC status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read discover models response: %w", err)
	}

	c.checkSetCookies(resp.Headers)

	discovered := parseUserStatusResponse(string(bodyBytes))
	if len(discovered) == 0 {
		return nil, errors.New("discover models RPC returned no usable models")
	}

	c.setDiscoveredSpecs(discovered)
	return discovered, nil
}

// parseUserStatusResponse walks the batchexecute frames and builds an
// alias -> ModelSpec map. Port of AvailableModel.parse_models_from_rpc().
func parseUserStatusResponse(raw string) map[string]ModelSpec {
	result := make(map[string]ModelSpec)

	for _, part := range extractJSONFrames(raw) {
		partList, ok := part.([]any)
		if !ok || len(partList) < 3 {
			continue
		}
		if rpcID, ok := partList[1].(string); ok && rpcID != "" && rpcID != RPCGetUserStatus {
			continue
		}
		bodyStr, ok := partList[2].(string)
		if !ok || bodyStr == "" {
			continue
		}
		var partBody []any
		if err := json.Unmarshal([]byte(bodyStr), &partBody); err != nil {
			continue
		}

		modelsList, ok := nestedValue(partBody, 15).([]any)
		if !ok || len(modelsList) == 0 {
			continue
		}

		tierFlags, _ := nestedValue(partBody, 16).([]any)
		capabilityFlags, _ := nestedValue(partBody, 17).([]any)
		capacity, capacityField := computeCapacity(tierFlags, capabilityFlags)

		for _, mData := range modelsList {
			spec, aliases, ok := specFromRPC(mData, capacity, capacityField)
			if !ok {
				continue
			}
			for _, alias := range aliases {
				result[alias] = spec
			}
		}
		if len(result) > 0 {
			break
		}
	}

	return result
}

// specFromRPC reads one entry of part_body[15] into a ModelSpec plus its lookup
// aliases. Port of AvailableModel.from_rpc().
func specFromRPC(mData any, capacity, capacityField int) (ModelSpec, []string, bool) {
	list, ok := mData.([]any)
	if !ok || len(list) == 0 {
		return ModelSpec{}, nil, false
	}
	modelID, ok := nestedValue(list, 0).(string)
	if !ok || modelID == "" {
		return ModelSpec{}, nil, false
	}

	category := firstString(nestedValue(list, 1), nestedValue(list, 10))
	display := firstString(nestedValue(list, 11), nestedValue(list, 19), nestedValue(list, 1))

	modelNumber := 1
	if n, ok := toInt(nestedValue(list, 17)); ok {
		modelNumber = n
	} else if n, ok := toInt(nestedValue(list, 9)); ok {
		modelNumber = n
	}

	_, aliases := deriveNameAndAliases(modelID, category, display)
	return ModelSpec{
		ModelID:       modelID,
		Capacity:      capacity,
		CapacityField: capacityField,
		ModelNumber:   modelNumber,
		AdvancedOnly:  capacity != 1 || capacityField != capacityFieldDefault,
	}, aliases, true
}

// extractJSONFrames splits a response into the elements of its frames,
// flattening one array level so callers see individual RPC envelopes.
//
// Mirrors extract_json_from_response() in gemini-webapi's utils/parsing.py:
// length-prefixed frames first, then one whole JSON document, then plain NDJSON -
// batchexecute replies are not always length-prefixed.
func extractJSONFrames(raw string) []any {
	body, _ := stripXSSIPrefix(strings.TrimLeft(raw, "\r\n"))

	if parts, ok := scanLengthPrefixedFrames(body); ok {
		return parts
	}

	trimmed := strings.TrimSpace(body)
	var whole any
	if err := json.Unmarshal([]byte(trimmed), &whole); err == nil {
		if list, ok := whole.([]any); ok {
			return list
		}
		return []any{whole}
	}

	var parts []any
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var parsed any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			continue
		}
		if list, ok := parsed.([]any); ok {
			parts = append(parts, list...)
			continue
		}
		parts = append(parts, parsed)
	}
	return parts
}

// scanLengthPrefixedFrames reports ok=false when the body carries no readable
// length marker at its start, so the caller can try the other encodings.
func scanLengthPrefixedFrames(body string) ([]any, bool) {
	var parts []any
	buf := body
	found := false

	for {
		payload, rest, status := scanFrame(buf)
		switch status {
		case frameFound:
			found = true
			buf = rest
			appendFramePayload(&parts, payload)
		case frameNeedMore:
			return parts, found
		default:
			if !found {
				return parts, false
			}
			off := resyncOffset(buf)
			if off <= 0 {
				return parts, true
			}
			buf = buf[off:]
		}
	}
}

func appendFramePayload(parts *[]any, payload string) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return
	}
	var parsed any
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		if idx := strings.LastIndexByte(payload, ']'); idx > 0 {
			if err2 := json.Unmarshal([]byte(payload[:idx+1]), &parsed); err2 == nil {
				if list, ok := parsed.([]any); ok {
					*parts = append(*parts, list...)
					return
				}
				*parts = append(*parts, parsed)
				return
			}
		}
		return
	}
	if list, ok := parsed.([]any); ok {
		*parts = append(*parts, list...)
		return
	}
	*parts = append(*parts, parsed)
}

// nestedValue walks a decoded JSON structure by list index, returning nil when
// the path does not resolve. Counterpart to get_nested_value() in
// gemini-webapi's utils/parsing.py.
func nestedValue(data any, path ...int) any {
	current := data
	for _, idx := range path {
		list, ok := current.([]any)
		if !ok || idx < 0 || idx >= len(list) {
			return nil
		}
		current = list[idx]
	}
	return current
}

// firstString returns the first value that is a non-empty string.
func firstString(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// toInt narrows a decoded JSON number to an int.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}
