package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"fraud-shield/internal/models"
	"fraud-shield/internal/scoring"
	"fraud-shield/internal/store"
)

func newTestServer() (*Server, *store.MemoryStore) {
	st := store.NewMemoryStore()
	return NewServer(scoring.NewEngine(scoring.DefaultConfig()), st), st
}

func do(t *testing.T, s *Server, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	return rec
}

func TestHealthOK(t *testing.T) {
	s, _ := newTestServer()
	rec := do(t, s, http.MethodGet, "/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

type failingStore struct{ *store.MemoryStore }

func (failingStore) Ping(context.Context) error { return errors.New("db down") }

func TestHealthDegradedWhenStoreDown(t *testing.T) {
	s := NewServer(scoring.NewEngine(scoring.DefaultConfig()), failingStore{store.NewMemoryStore()})
	rec := do(t, s, http.MethodGet, "/health", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503", rec.Code)
	}
}

func TestScoreValidation(t *testing.T) {
	s, _ := newTestServer()
	if rec := do(t, s, http.MethodPost, "/v1/transactions/score", map[string]any{"amount": 5}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing account_id: status=%d want 400", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/transactions/score", bytes.NewBufferString("{not json"))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json: status=%d want 400", rec.Code)
	}
}

func TestScoreUnflaggedIsNotPersisted(t *testing.T) {
	s, st := newTestServer()
	rec := do(t, s, http.MethodPost, "/v1/transactions/score", map[string]any{"account_id": "a", "amount": 10})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var res models.ScoreResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Flagged || res.TransactionID == "" {
		t.Fatalf("unexpected result %+v", res)
	}
	alerts, _ := st.ListAlerts(context.Background(), "", 10)
	if len(alerts) != 0 {
		t.Fatalf("unflagged result must not be persisted, got %d alerts", len(alerts))
	}
}

func TestFlaggedTransactionIsPersistedAndListed(t *testing.T) {
	s, _ := newTestServer()

	if rec := do(t, s, http.MethodPost, "/v1/ring/flag", map[string]string{"account_id": "bad"}); rec.Code != http.StatusOK {
		t.Fatalf("ring flag status=%d", rec.Code)
	}
	do(t, s, http.MethodPost, "/v1/transactions/score", map[string]any{"account_id": "bad", "device_id": "d1"})

	rec := do(t, s, http.MethodPost, "/v1/transactions/score",
		map[string]any{"account_id": "mule", "device_id": "d1", "amount": 20000})
	var res models.ScoreResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if !res.Flagged || res.Score < 50 || len(res.Reasons) == 0 {
		t.Fatalf("expected flagged result with reasons, got %+v", res)
	}

	rec = do(t, s, http.MethodGet, "/v1/alerts?account_id=mule&limit=5", nil)
	var alerts []models.Alert
	_ = json.Unmarshal(rec.Body.Bytes(), &alerts)
	if len(alerts) != 1 || alerts[0].TransactionID != res.TransactionID {
		t.Fatalf("alerts=%+v", alerts)
	}
}

func TestListAlertsEmptyIsJSONArray(t *testing.T) {
	s, _ := newTestServer()
	rec := do(t, s, http.MethodGet, "/v1/alerts", nil)
	if got := bytes.TrimSpace(rec.Body.Bytes()); string(got) != "[]" {
		t.Fatalf("body=%s want []", got)
	}
}

func TestRulesListAndUpsert(t *testing.T) {
	s, _ := newTestServer()

	var before []models.Rule
	_ = json.Unmarshal(do(t, s, http.MethodGet, "/v1/rules", nil).Body.Bytes(), &before)
	if len(before) != 4 {
		t.Fatalf("default rules=%d want 4", len(before))
	}

	if rec := do(t, s, http.MethodPost, "/v1/rules", map[string]any{"name": "no id"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("missing id: status=%d want 400", rec.Code)
	}

	up := map[string]any{"id": "large_amount", "name": "Large", "weight": 80, "enabled": true, "threshold": 100}
	if rec := do(t, s, http.MethodPost, "/v1/rules", up); rec.Code != http.StatusOK {
		t.Fatalf("upsert status=%d", rec.Code)
	}
	rec := do(t, s, http.MethodPost, "/v1/transactions/score", map[string]any{"account_id": "a", "amount": 500})
	var res models.ScoreResult
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if res.Score != 80 || !res.Flagged {
		t.Fatalf("rule update should change scoring immediately, got %+v", res)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	s, _ := newTestServer()
	if rec := do(t, s, http.MethodGet, "/v1/transactions/score", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d want 405", rec.Code)
	}
}
