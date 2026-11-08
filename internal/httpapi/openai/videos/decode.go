package videos

import (
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/mediacompat"
)

const multipartMemoryLimit = 32 << 20

var (
	errTooLarge         = errors.New("request body too large")
	errInvalidJSON      = errors.New("invalid json")
	errInvalidMultipart = errors.New("invalid multipart form")
)

func (h *Handler) readCreatePayload(w http.ResponseWriter, r *http.Request) (map[string]any, []mediacompat.Reference, error) {
	if isMultipart(r) {
		return parseForm(w, r)
	}
	r.Body = http.MaxBytesReader(w, r.Body, shared.GeneralMaxSize)
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") {
			return nil, nil, errTooLarge
		}
		return nil, nil, errInvalidJSON
	}
	return payload, nil, nil
}

func parseForm(w http.ResponseWriter, r *http.Request) (map[string]any, []mediacompat.Reference, error) {
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
	for _, key := range []string{"model", "prompt", "size", "seconds"} {
		if value := strings.TrimSpace(r.FormValue(key)); value != "" {
			payload[key] = value
		}
	}
	var references []mediacompat.Reference
	for _, field := range []string{"input_reference", "image"} {
		for _, header := range r.MultipartForm.File[field] {
			reference, err := readReference(header)
			if err != nil {
				return nil, nil, err
			}
			references = append(references, reference)
		}
	}
	return payload, references, nil
}

func readReference(header *multipart.FileHeader) (mediacompat.Reference, error) {
	if header.Size > mediacompat.MaxReferenceBytes {
		return mediacompat.Reference{}, errTooLarge
	}
	file, err := header.Open()
	if err != nil {
		return mediacompat.Reference{}, err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, mediacompat.MaxReferenceBytes+1))
	if err != nil {
		return mediacompat.Reference{}, err
	}
	if len(data) == 0 {
		return mediacompat.Reference{}, errors.New("uploaded reference media is empty")
	}
	if len(data) > mediacompat.MaxReferenceBytes {
		return mediacompat.Reference{}, errTooLarge
	}
	contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	return mediacompat.Reference{Filename: header.Filename, ContentType: contentType, Data: data}, nil
}

func isMultipart(r *http.Request) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "multipart/form-data")
}

func writeDecodeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errTooLarge):
		shared.WriteOpenAIError(w, http.StatusRequestEntityTooLarge, errTooLarge.Error())
	case errors.Is(err, errInvalidMultipart):
		shared.WriteOpenAIError(w, http.StatusBadRequest, err.Error())
	default:
		shared.WriteOpenAIError(w, http.StatusBadRequest, errInvalidJSON.Error())
	}
}

func writeNormalizeError(w http.ResponseWriter, err error) {
	if errors.Is(err, mediacompat.ErrInvalidMediaRequest) {
		message := strings.TrimPrefix(err.Error(), mediacompat.ErrInvalidMediaRequest.Error()+": ")
		shared.WriteOpenAIError(w, http.StatusBadRequest, message)
		return
	}
	shared.WriteOpenAIError(w, http.StatusBadRequest, err.Error())
}
