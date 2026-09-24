package configmgmt

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestKeyEndpointsPreserveStructuredMetadata(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"api_keys":[{"key":"k1","name":"primary","remark":"prod"}]
	}`)

	r := chi.NewRouter()
	r.Post("/admin/keys", h.addKey)
	r.Put("/admin/keys/{key}", h.updateKey)
	r.Delete("/admin/keys/{key}", h.deleteKey)

	addBody := []byte(`{"key":"k2","name":"secondary","remark":"staging"}`)
	addReq := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewReader(addBody))
	addRec := httptest.NewRecorder()
	r.ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", addRec.Code, addRec.Body.String())
	}

	snap := h.Store.Snapshot()
	if len(snap.APIKeys) != 2 {
		t.Fatalf("unexpected api keys after add: %#v", snap.APIKeys)
	}
	if snap.APIKeys[0].Name != "primary" || snap.APIKeys[0].Remark != "prod" {
		t.Fatalf("existing metadata was lost after add: %#v", snap.APIKeys[0])
	}
	if snap.APIKeys[1].Name != "secondary" || snap.APIKeys[1].Remark != "staging" {
		t.Fatalf("new metadata was lost after add: %#v", snap.APIKeys[1])
	}

	updateBody := map[string]any{
		"name":   "primary-updated",
		"remark": "prod-updated",
	}
	updateBytes, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/admin/keys/k1", bytes.NewReader(updateBytes))
	updateRec := httptest.NewRecorder()
	r.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	snap = h.Store.Snapshot()
	if len(snap.APIKeys) != 2 {
		t.Fatalf("unexpected api keys after update: %#v", snap.APIKeys)
	}
	if snap.APIKeys[0].Key != "k1" || snap.APIKeys[0].Name != "primary-updated" || snap.APIKeys[0].Remark != "prod-updated" {
		t.Fatalf("metadata update did not persist: %#v", snap.APIKeys[0])
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/admin/keys/k1", nil)
	deleteRec := httptest.NewRecorder()
	r.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	snap = h.Store.Snapshot()
	if len(snap.APIKeys) != 1 || snap.APIKeys[0].Key != "k2" {
		t.Fatalf("unexpected api keys after delete: %#v", snap.APIKeys)
	}
	if len(snap.Keys) != 1 || snap.Keys[0] != "k2" {
		t.Fatalf("unexpected legacy keys after delete: %#v", snap.Keys)
	}
}

func TestKeyEndpointsPersistToolsEnabled(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"api_keys":[{"key":"k1","name":"primary","remark":"prod"}]
	}`)

	r := chi.NewRouter()
	r.Post("/admin/keys", h.addKey)
	r.Put("/admin/keys/{key}", h.updateKey)

	addBody := []byte(`{"key":"k2","name":"secondary","remark":"staging","tools_enabled":true}`)
	addReq := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewReader(addBody))
	addRec := httptest.NewRecorder()
	r.ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusOK {
		t.Fatalf("add status=%d body=%s", addRec.Code, addRec.Body.String())
	}

	snap := h.Store.Snapshot()
	if len(snap.APIKeys) != 2 {
		t.Fatalf("unexpected api keys after add: %#v", snap.APIKeys)
	}
	if snap.APIKeys[0].ToolsEnabled != false {
		t.Fatalf("existing key tools_enabled should default false: %#v", snap.APIKeys[0])
	}
	if snap.APIKeys[1].ToolsEnabled != true {
		t.Fatalf("new key tools_enabled was not persisted: %#v", snap.APIKeys[1])
	}
	if !h.Store.APIKeyToolsEnabled("k2") {
		t.Fatalf("APIKeyToolsEnabled should return true for k2")
	}
	if h.Store.APIKeyToolsEnabled("k1") {
		t.Fatalf("APIKeyToolsEnabled should return false for k1")
	}

	updateBody := map[string]any{
		"tools_enabled": true,
	}
	updateBytes, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/admin/keys/k1", bytes.NewReader(updateBytes))
	updateRec := httptest.NewRecorder()
	r.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	snap = h.Store.Snapshot()
	if snap.APIKeys[0].ToolsEnabled != true {
		t.Fatalf("tools_enabled update did not persist: %#v", snap.APIKeys[0])
	}
	if !h.Store.APIKeyToolsEnabled("k1") {
		t.Fatalf("APIKeyToolsEnabled should return true for k1 after update")
	}
}

func TestAddKeyWithPolicyAssignments(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[{"email":"user1@example.com"}],
		"api_keys":[]
	}`)

	r := chi.NewRouter()
	r.Post("/admin/keys", h.addKey)

	body := []byte(`{
		"key":"sk-test",
		"name":"my-key",
		"accounts":["user1@example.com"],
		"models":["deepseek-v4-flash"],
		"quota_tokens":50000
	}`)
	req := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	snap := h.Store.Snapshot()
	if len(snap.APIKeys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(snap.APIKeys))
	}
	k := snap.APIKeys[0]
	if !slices.Equal(k.Accounts, []string{"user1@example.com"}) {
		t.Errorf("unexpected accounts: %#v", k.Accounts)
	}
	if !slices.Equal(k.Models, []string{"deepseek-v4-flash"}) {
		t.Errorf("unexpected models: %#v", k.Models)
	}
	if k.QuotaTokens != 50000 {
		t.Errorf("unexpected quota: %d", k.QuotaTokens)
	}
}

func TestAddKeyPolicyValidationErrors(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[{"email":"user1@example.com"}],
		"api_keys":[]
	}`)

	r := chi.NewRouter()
	r.Post("/admin/keys", h.addKey)

	// 1. Unknown account -> 400
	{
		body := []byte(`{"key":"k1","accounts":["unknown@example.com"]}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for unknown account, got %d", rec.Code)
		}
	}

	// 2. Unknown model -> 400
	{
		body := []byte(`{"key":"k2","models":["non-existent-model"]}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for unknown model, got %d", rec.Code)
		}
	}

	// 3. Negative quota -> 400
	{
		body := []byte(`{"key":"k3","quota_tokens":-100}`)
		req := httptest.NewRequest(http.MethodPost, "/admin/keys", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for negative quota, got %d", rec.Code)
		}
	}
}

func TestUpdateKeyReplaceAndClearPolicy(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[{"email":"acc1@test.com"},{"email":"acc2@test.com"}],
		"api_keys":[{
			"key":"k1",
			"accounts":["acc1@test.com"],
			"models":["deepseek-v4-flash"],
			"quota_tokens":1000
		}]
	}`)

	r := chi.NewRouter()
	r.Put("/admin/keys/{key}", h.updateKey)

	// 1. Replace with acc2, pro model, 2000 quota
	{
		body := []byte(`{
			"accounts":["acc2@test.com"],
			"models":["deepseek-v4-pro"],
			"quota_tokens":2000
		}`)
		req := httptest.NewRequest(http.MethodPut, "/admin/keys/k1", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		snap := h.Store.Snapshot()
		if !slices.Equal(snap.APIKeys[0].Accounts, []string{"acc2@test.com"}) {
			t.Errorf("accounts not replaced: %#v", snap.APIKeys[0].Accounts)
		}
		if !slices.Equal(snap.APIKeys[0].Models, []string{"deepseek-v4-pro"}) {
			t.Errorf("models not replaced: %#v", snap.APIKeys[0].Models)
		}
		if snap.APIKeys[0].QuotaTokens != 2000 {
			t.Errorf("quota not replaced: %d", snap.APIKeys[0].QuotaTokens)
		}
	}

	// 2. Clear policy (empty arrays, quota 0)
	{
		body := []byte(`{
			"accounts":[],
			"models":[],
			"quota_tokens":0
		}`)
		req := httptest.NewRequest(http.MethodPut, "/admin/keys/k1", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		snap := h.Store.Snapshot()
		if len(snap.APIKeys[0].Accounts) != 0 {
			t.Errorf("expected empty accounts, got %#v", snap.APIKeys[0].Accounts)
		}
		if len(snap.APIKeys[0].Models) != 0 {
			t.Errorf("expected empty models, got %#v", snap.APIKeys[0].Models)
		}
		if snap.APIKeys[0].QuotaTokens != 0 {
			t.Errorf("expected quota 0, got %d", snap.APIKeys[0].QuotaTokens)
		}
	}

	// 3. Negative quota -> 400
	{
		body := []byte(`{"quota_tokens":-50}`)
		req := httptest.NewRequest(http.MethodPut, "/admin/keys/k1", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for negative quota, got %d", rec.Code)
		}
	}
}

func TestUpdateConfigStrictPolicyValidation(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[{"email":"user1@test.com"}],
		"api_keys":[]
	}`)

	r := chi.NewRouter()
	r.Post("/admin/config", h.updateConfig)

	// Invalid model in api_keys -> 400
	{
		payload := map[string]any{
			"api_keys": []any{
				map[string]any{
					"key":    "k1",
					"models": []any{"invalid-model"},
				},
			},
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/admin/config", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("expected 400 on invalid model in updateConfig, got %d", rec.Code)
		}
	}

	// Valid policy in updateConfig -> 200
	{
		payload := map[string]any{
			"api_keys": []any{
				map[string]any{
					"key":          "k1",
					"accounts":     []any{"user1@test.com"},
					"models":       []any{"deepseek-v4-flash"},
					"quota_tokens": 10000,
				},
			},
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/admin/config", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 on valid updateConfig, got %d body=%s", rec.Code, rec.Body.String())
		}
		snap := h.Store.Snapshot()
		if len(snap.APIKeys) != 1 || snap.APIKeys[0].QuotaTokens != 10000 {
			t.Errorf("unexpected snap after updateConfig: %+v", snap.APIKeys)
		}
	}
}

func TestBatchImportMergePrecedence(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"api_keys":[{
			"key":"k1",
			"name":"local-name",
			"accounts":["local-acc"],
			"models":["deepseek-v4-flash"],
			"quota_tokens":5000
		}]
	}`)

	r := chi.NewRouter()
	r.Post("/admin/batch-import", h.batchImport)

	// Incoming key k1 has different accounts, models, quota, and k2 is new
	payload := map[string]any{
		"api_keys": []any{
			map[string]any{
				"key":          "k1",
				"name":         "incoming-name",
				"accounts":     []any{"incoming-acc"},
				"models":       []any{"incoming-model"},
				"quota_tokens": 99999,
			},
			map[string]any{
				"key":          "k2",
				"accounts":     []any{"k2-acc"},
				"models":       []any{"k2-model"},
				"quota_tokens": 12345,
			},
		},
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/admin/batch-import", bytes.NewReader(b))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch-import status=%d body=%s", rec.Code, rec.Body.String())
	}

	snap := h.Store.Snapshot()
	if len(snap.APIKeys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(snap.APIKeys))
	}

	// k1: local policy takes precedence
	k1 := snap.APIKeys[0]
	if !slices.Equal(k1.Accounts, []string{"local-acc"}) {
		t.Errorf("expected local accounts preserved, got %#v", k1.Accounts)
	}
	if !slices.Equal(k1.Models, []string{"deepseek-v4-flash"}) {
		t.Errorf("expected local models preserved, got %#v", k1.Models)
	}
	if k1.QuotaTokens != 5000 {
		t.Errorf("expected local quota preserved, got %d", k1.QuotaTokens)
	}

	// k2: new key gets incoming policy
	k2 := snap.APIKeys[1]
	if !slices.Equal(k2.Accounts, []string{"k2-acc"}) {
		t.Errorf("expected imported accounts for k2, got %#v", k2.Accounts)
	}
	if !slices.Equal(k2.Models, []string{"k2-model"}) {
		t.Errorf("expected imported models for k2, got %#v", k2.Models)
	}
	if k2.QuotaTokens != 12345 {
		t.Errorf("expected imported quota for k2, got %d", k2.QuotaTokens)
	}
}
