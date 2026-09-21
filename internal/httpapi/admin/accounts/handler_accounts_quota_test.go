package accounts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAccountQuota_NotFound(t *testing.T) {
	router := newHTTPAdminHarness(t, `{"accounts":[{"email":"u@example.com","password":"pwd"}]}`, &testingDSMock{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodGet, "/accounts/nonexistent/quota", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent account, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetAccountQuota_NotGemini(t *testing.T) {
	router := newHTTPAdminHarness(t, `{"accounts":[{"email":"u@example.com","password":"pwd"}]}`, &testingDSMock{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodGet, "/accounts/u@example.com/quota", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-Gemini account, got %d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if detail, _ := res["detail"].(string); detail != "仅支持查询 Gemini 账号的配额与用量" {
		t.Fatalf("unexpected detail: %s", detail)
	}
}

func TestGetAccountQuota_MissingCookies(t *testing.T) {
	router := newHTTPAdminHarness(t, `{"accounts":[{"name":"gem-empty","provider":"gemini"}]}`, &testingDSMock{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, adminReq(http.MethodGet, "/accounts/gem-empty/quota", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for Gemini account with empty cookies, got %d body=%s", rec.Code, rec.Body.String())
	}
}
