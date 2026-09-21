package geminiweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path/filepath"
	"strings"

	httpcloak "github.com/sardanioss/httpcloak/client"
)

type UploadedFile struct {
	ID          string
	Filename    string
	ContentType string
	Size        int64
}

// UploadFile uploads file content to Google's server and returns its file path/ID.
func (c *Client) UploadFile(ctx context.Context, filename string, data []byte, contentType string) (*UploadedFile, error) {
	session := c.Session()
	if session.PushID == "" {
		return nil, errors.New("gemini push_id (qKIAYe) not available in session")
	}

	if contentType == "" {
		contentType = detectContentType(filename, data)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, filepath.Base(filename)))
	h.Set("Content-Type", contentType)

	part, err := writer.CreatePart(h)
	if err != nil {
		return nil, fmt.Errorf("failed to create multipart part: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write file data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	headers := c.BuildDefaultHeaders()
	headers["Content-Type"] = []string{writer.FormDataContentType()}
	headers["Push-ID"] = []string{session.PushID}

	req := &httpcloak.Request{
		Method:  http.MethodPost,
		URL:     UploadURL,
		Headers: headers,
		Body:    &body,
	}

	resp, err := c.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("upload request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upload failed with status %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	fileIDBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read upload response: %w", err)
	}
	fileID := strings.TrimSpace(string(fileIDBytes))
	if fileID == "" {
		return nil, errors.New("empty file identifier received from upload")
	}

	c.checkSetCookies(resp.Headers)

	return &UploadedFile{
		ID:          fileID,
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(data)),
	}, nil
}

func detectContentType(filename string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".pdf":
		return "application/pdf"
	case ".txt":
		return "text/plain"
	}
	if len(data) > 512 {
		return http.DetectContentType(data[:512])
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return "application/octet-stream"
}
