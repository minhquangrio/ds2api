package images

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/mediacompat"
)

const (
	multipartMemoryLimit = 32 << 20
	// maxReferenceBytes caps one uploaded reference at the API boundary so an
	// oversized file fails before it is base64-encoded toward the worker.
	maxReferenceBytes = mediacompat.MaxReferenceBytes
)

// referenceFormFields are the accepted multipart file names for reference
// media. "mask" is deliberately absent: Muse edits consume reference images,
// and silently ignoring a mask would return a different picture than the
// caller asked for, so it is reported as unsupported instead.
var referenceFormFields = []string{"image", "image[]", "file", "file[]"}

var (
	errTooLarge         = errors.New("request body too large")
	errInvalidMultipart = errors.New("invalid multipart form")
	errInvalidJSON      = errors.New("invalid json")
	errUnsupportedField = errors.New("mask is not supported by this provider")
)

// parseJSONBody decodes a JSON images request and lifts inline data-URI
// references out of it.
func parseJSONBody(w http.ResponseWriter, r *http.Request) (map[string]any, []mediacompat.Reference, error) {
	r.Body = http.MaxBytesReader(w, r.Body, shared.GeneralMaxSize)
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			return nil, nil, errTooLarge
		}
		return nil, nil, errInvalidJSON
	}
	references, err := referencesFromDataURI(payload)
	if err != nil {
		return nil, nil, err
	}
	return payload, references, nil
}

// parseFormBody decodes a multipart images request (OpenAI /v1/images/edits).
func parseFormBody(w http.ResponseWriter, r *http.Request) (map[string]any, []mediacompat.Reference, error) {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		return nil, nil, errNotMultipart
	}
	r.Body = http.MaxBytesReader(w, r.Body, shared.UploadMaxSize)
	if err := r.ParseMultipartForm(multipartMemoryLimit); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			return nil, nil, errTooLarge
		}
		return nil, nil, errInvalidMultipart
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	payload := map[string]any{}
	for _, key := range []string{"model", "prompt", "size", "response_format", "quality", "user"} {
		if value := strings.TrimSpace(r.FormValue(key)); value != "" {
			payload[key] = value
		}
	}
	if raw := strings.TrimSpace(r.FormValue("n")); raw != "" {
		count, err := strconv.Atoi(raw)
		if err != nil {
			return nil, nil, errors.New("n must be a number")
		}
		payload["n"] = count
	}
	if len(r.MultipartForm.Value["mask"]) > 0 {
		return nil, nil, errUnsupportedField
	}

	references, err := readFormFiles(r)
	if err != nil {
		return nil, nil, err
	}
	return payload, references, nil
}

var errNotMultipart = errors.New("content-type must be multipart/form-data")

func readFormFiles(r *http.Request) ([]mediacompat.Reference, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, nil
	}
	var references []mediacompat.Reference
	for _, field := range referenceFormFields {
		for _, header := range r.MultipartForm.File[field] {
			reference, err := readReference(header)
			if err != nil {
				return nil, err
			}
			references = append(references, reference)
		}
	}
	return references, nil
}

func readReference(header *multipart.FileHeader) (mediacompat.Reference, error) {
	if header.Size > maxReferenceBytes {
		return mediacompat.Reference{}, errTooLarge
	}
	file, err := header.Open()
	if err != nil {
		return mediacompat.Reference{}, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, maxReferenceBytes+1))
	if err != nil {
		return mediacompat.Reference{}, err
	}
	if len(data) > maxReferenceBytes {
		return mediacompat.Reference{}, errTooLarge
	}
	if len(data) == 0 {
		return mediacompat.Reference{}, errors.New("uploaded reference image is empty")
	}
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	// Many clients (and Go's own multipart writer) label file parts
	// application/octet-stream; sniffing keeps valid PNG/JPEG uploads working.
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	return mediacompat.Reference{Filename: header.Filename, ContentType: contentType, Data: data}, nil
}

// referencesFromDataURI lifts inline base64 images out of a JSON body.
func referencesFromDataURI(payload map[string]any) ([]mediacompat.Reference, error) {
	value, ok := payload["image"]
	if !ok || value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		items = []any{value}
	}
	references := make([]mediacompat.Reference, 0, len(items))
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, errors.New("image entries must be data URLs")
		}
		reference, err := parseDataURI(text, index)
		if err != nil {
			return nil, err
		}
		references = append(references, reference)
	}
	return references, nil
}

func parseDataURI(value string, index int) (mediacompat.Reference, error) {
	if !strings.HasPrefix(strings.ToLower(value), "data:") {
		return mediacompat.Reference{}, errors.New("image must be a data: URL when sent as JSON")
	}
	comma := strings.Index(value, ",")
	if comma < 0 {
		return mediacompat.Reference{}, errors.New("image data URL is malformed")
	}
	metadata := value[len("data:"):comma]
	encoded := value[comma+1:]
	contentType := metadata
	if idx := strings.LastIndex(metadata, ";"); idx >= 0 {
		contentType = metadata[:idx]
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return mediacompat.Reference{}, errors.New("image data URL is not valid base64")
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		contentType = http.DetectContentType(data)
	}
	return mediacompat.Reference{
		Filename:    "reference-" + strconv.Itoa(index+1),
		ContentType: contentType,
		Data:        data,
	}, nil
}
