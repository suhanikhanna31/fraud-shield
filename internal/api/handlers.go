// Package api exposes the fraud-shield scoring engine over a small REST API.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"fraud-shield/internal/models"
	"fraud-shield/internal/scoring"
	"fraud-shield/internal/store"
)

type Server struct {
	Engine *scoring.Engine
	Store  store.Store
}

func NewServer(engine *scoring.Engine, st store.Store) *Server {
	return &Server{Engine: engine, Store: st}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /v1/transactions/score", s.handleScoreTransaction)
	mux.HandleFunc("GET /v1/alerts", s.handleListAlerts)
	mux.HandleFunc("GET /v1/rules", s.handleListRules)
	mux.HandleFunc("POST /v1/rules", s.handleUpsertRule)
	mux.HandleFunc("POST /v1/ring/flag", s.handleFlagRingAccount)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleScoreTransaction accepts a transaction, runs it through the scoring
// engine, persists an alert if flagged, and returns the score breakdown.
func (s *Server) handleScoreTransaction(w http.ResponseWriter, r *http.Request) {
	var tx models.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if tx.AccountID == "" {
		writeError(w, http.StatusBadRequest, "account_id is required")
		return
	}
	if tx.ID == "" {
		tx.ID = uuid.NewString()
	}
	if tx.Timestamp.IsZero() {
		tx.Timestamp = time.Now().UTC()
	}

	result := s.Engine.Score(tx)

	if result.Flagged {
		alert := models.Alert{
			ID:            uuid.NewString(),
			TransactionID: tx.ID,
			AccountID:     tx.AccountID,
			Score:         result.Score,
			Reasons:       result.Reasons,
			CreatedAt:     time.Now().UTC(),
		}
		ctx, cancel := contextWithTimeout(r)
		defer cancel()
		if err := s.Store.SaveAlert(ctx, alert); err != nil {
			log.Printf("failed to persist alert for tx %s: %v", tx.ID, err)
			// Scoring succeeded even if persistence failed; surface the
			// result to the caller but log the storage error.
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			limit = parsed
		}
	}
	ctx, cancel := contextWithTimeout(r)
	defer cancel()
	alerts, err := s.Store.ListAlerts(ctx, accountID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list alerts: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Engine.Rules())
}

func (s *Server) handleUpsertRule(w http.ResponseWriter, r *http.Request) {
	var rule models.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if rule.ID == "" || rule.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	s.Engine.AddRule(rule)
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleFlagRingAccount(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID string `json:"account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AccountID == "" {
		writeError(w, http.StatusBadRequest, "account_id is required")
		return
	}
	s.Engine.MarkRingAccount(body.AccountID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "flagged", "account_id": body.AccountID})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
