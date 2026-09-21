package geminiweb

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	ErrUnauthenticated   = errors.New("gemini unauthenticated: invalid session or expired cookies")
	ErrRotationThrottled = errors.New("gemini cookie rotation throttled: rotation attempted too recently")
)

type RotateSessionOptions struct {
	Force         bool
	SkipDiscovery bool
	ReuseFresh    bool
}

func (c *Client) getAppURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.appURL != "" {
		return c.appURL
	}
	return AppURL
}

func (c *Client) getRotateURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.rotateURL != "" {
		return c.rotateURL
	}
	return RotateCookiesURL
}

func (c *Client) getStreamGenerateURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.streamGenURL != "" {
		return c.streamGenURL
	}
	return StreamGenerateURL
}

// RotateAndInitSession rotates the __Secure-1PSIDTS cookie and re-initializes the session.
// It uses a dedicated mutex (single-flight) and enforces cooldown guards to prevent 429 errors.
func (c *Client) RotateAndInitSession(ctx context.Context, opts RotateSessionOptions) error {
	c.rotateMu.Lock()
	defer c.rotateMu.Unlock()

	minCooldown := 60 * time.Second
	if opts.Force {
		minCooldown = 10 * time.Second
	}
	if !c.lastRotated.IsZero() && time.Since(c.lastRotated) < minCooldown {
		if opts.ReuseFresh && time.Since(c.lastRotated) < 5*time.Second {
			return nil
		}
		return ErrRotationThrottled
	}

	rotateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if _, err := c.RotateCookies(rotateCtx); err != nil {
		return err
	}
	c.lastRotated = time.Now()

	if _, err := c.InitSession(rotateCtx, InitSessionOptions{SkipDiscovery: opts.SkipDiscovery}); err != nil {
		return err
	}
	return nil
}

// StreamGenerateWithRetry executes StreamGenerate, retrying once if unauthenticated.
func (c *Client) StreamGenerateWithRetry(ctx context.Context, prompt string, opts GenerateOptions) (*StreamReader, error) {
	reader, err := c.StreamGenerate(ctx, prompt, opts)
	if err == nil {
		return reader, nil
	}
	if !errors.Is(err, ErrUnauthenticated) {
		return nil, err
	}

	// Attempt single-flight self-healing rotation (reusing recently rotated session if any)
	if rotErr := c.RotateAndInitSession(ctx, RotateSessionOptions{SkipDiscovery: true, ReuseFresh: true}); rotErr != nil {
		return nil, err
	}

	return c.StreamGenerate(ctx, prompt, opts)
}

// GenerateWithRetry executes Generate, retrying once if unauthenticated.
func (c *Client) GenerateWithRetry(ctx context.Context, prompt string, opts GenerateOptions) (*GenerateResult, error) {
	reader, err := c.StreamGenerateWithRetry(ctx, prompt, opts)
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
