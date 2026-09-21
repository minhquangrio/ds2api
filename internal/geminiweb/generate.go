package geminiweb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	httpcloak "github.com/sardanioss/httpcloak/client"
)

type GenerateOptions struct {
	Model    string
	Thinking bool
	CID      string
	RID      string
	RCID     string
	FileIDs  []string
}

type GenerateResult struct {
	Text     string
	Thoughts string
	CID      string
	RID      string
	RCID     string
}

type StreamReader struct {
	body    io.ReadCloser
	parser  *StreamParser
	pending []ParsedChunk
	closed  bool
}

func (r *StreamReader) ReadChunk() (*ParsedChunk, error) {
	if len(r.pending) > 0 {
		c := r.pending[0]
		r.pending = r.pending[1:]
		return &c, nil
	}
	if r.closed || r.body == nil {
		return nil, io.EOF
	}

	buf := make([]byte, 4096)
	for {
		n, err := r.body.Read(buf)
		if n > 0 {
			chunks, parseErr := r.parser.Feed(buf[:n])
			if parseErr != nil {
				return nil, parseErr
			}
			if len(chunks) > 0 {
				c := chunks[0]
				if len(chunks) > 1 {
					r.pending = append(r.pending, chunks[1:]...)
				}
				return &c, nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				r.closed = true
			}
			return nil, err
		}
	}
}

func (r *StreamReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.body != nil {
		return r.body.Close()
	}
	return nil
}

// StreamGenerate executes Google Gemini StreamGenerate RPC and returns a StreamReader.
func (c *Client) StreamGenerate(ctx context.Context, prompt string, opts GenerateOptions) (*StreamReader, error) {
	session := c.Session()
	if session.AccessToken == "" {
		return nil, errors.New("gemini session not initialized")
	}

	// Resolved per account: model id and tier capacity come from session
	// discovery, not from a static table (see model_spec.go).
	spec := c.ResolveModelSpec(opts.Model)

	uuidVal := newUUID()
	clientSessionID := newUUID()

	var fileData any
	if len(opts.FileIDs) > 0 {
		var fileList [][]any
		for _, fid := range opts.FileIDs {
			fileList = append(fileList, []any{[]any{fid}})
		}
		fileData = fileList
	}

	messageContent := []any{
		prompt,
		0,
		nil,
		fileData,
		nil,
		nil,
		0,
	}

	innerReq := make([]any, 81)
	innerReq[0] = messageContent
	innerReq[1] = []any{session.Language}
	innerReq[2] = []any{opts.CID, opts.RID, opts.RCID, nil, nil, nil, nil, nil, nil, ""}
	innerReq[6] = []any{1}
	innerReq[7] = 1
	innerReq[10] = 1
	innerReq[11] = 0
	innerReq[17] = []any{[]any{0}}
	innerReq[18] = 0
	innerReq[27] = 1
	innerReq[30] = []any{4}
	innerReq[41] = []any{1}
	innerReq[53] = 0
	innerReq[59] = uuidVal
	innerReq[61] = []any{}
	innerReq[68] = 1
	innerReq[79] = spec.ModelNumber
	if opts.Thinking {
		innerReq[80] = 2
	} else {
		innerReq[80] = 1
	}

	innerJSON, err := json.Marshal(innerReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal inner_req: %w", err)
	}

	reqDataJSON, err := json.Marshal([]any{nil, string(innerJSON)})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal f.req: %w", err)
	}

	params := url.Values{
		"hl":     {session.Language},
		"_reqid": {"200000"},
		"rt":     {"c"},
		"bl":     {session.BuildLabel},
	}
	if session.SessionID != "" {
		params.Set("f.sid", session.SessionID)
	}

	genURL := c.getStreamGenerateURL() + "?" + params.Encode()

	modelHeader := BuildModelHeader(spec, opts.Thinking, clientSessionID)

	headers := c.BuildDefaultHeaders()
	headers["Content-Type"] = []string{"application/x-www-form-urlencoded;charset=utf-8"}
	headers[HeaderExtModel] = []string{modelHeader}
	headers[HeaderExtParam1] = []string{"[0]"}
	headers[HeaderExtParam2] = []string{"[0,0,0]"}
	headers[HeaderExtUUID] = []string{fmt.Sprintf(`["%s",1]`, uuidVal)}

	postForm := url.Values{
		"at":    {session.AccessToken},
		"f.req": {string(reqDataJSON)},
	}

	req := &httpcloak.Request{
		Method:  http.MethodPost,
		URL:     genURL,
		Headers: headers,
		Body:    strings.NewReader(postForm.Encode()),
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("gemini stream generate request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		buf := make([]byte, 1024)
		n, _ := resp.Body.Read(buf)
		_ = resp.Body.Close()
		errMsg := fmt.Sprintf("gemini stream generate status %d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), string(buf[:n]))
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("%w: %s", ErrUnauthenticated, errMsg)
		}
		return nil, errors.New(errMsg)
	}

	c.checkSetCookies(resp.Headers)

	return &StreamReader{
		body:   resp.Body,
		parser: NewStreamParser(),
	}, nil
}

// Generate collects all streaming chunks and returns the complete text and thoughts.
func (c *Client) Generate(ctx context.Context, prompt string, opts GenerateOptions) (*GenerateResult, error) {
	reader, err := c.StreamGenerate(ctx, prompt, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()

	var lastChunk *ParsedChunk
	for {
		chunk, err := reader.ReadChunk()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		if chunk != nil {
			lastChunk = chunk
			if chunk.IsFinished {
				break
			}
		}
	}

	if lastChunk == nil {
		return nil, errors.New("empty response from gemini generate")
	}

	return &GenerateResult{
		Text:     lastChunk.FullText,
		Thoughts: lastChunk.FullThought,
		CID:      lastChunk.CID,
		RID:      lastChunk.RID,
		RCID:     lastChunk.RCID,
	}, nil
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return strings.ToUpper(fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16])))
}
