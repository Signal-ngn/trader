package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Spot-Canvas/ledger/internal/api/middleware"
	"github.com/Spot-Canvas/ledger/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Check database
	if err := s.repo.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"error":  "database unreachable",
		})
		return
	}

	// Check NATS
	if s.nc != nil && !s.nc.IsConnected() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "error",
			"error":  "NATS disconnected",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAuthResolve returns the resolved tenant ID for the authenticated caller.
func (s *Server) handleAuthResolve(w http.ResponseWriter, r *http.Request) {
	tenantID := middleware.TenantIDFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{
		"tenant_id": tenantID.String(),
	})
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID := middleware.TenantIDFromContext(r.Context())
	accounts, err := s.repo.ListAccounts(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list accounts")
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (s *Server) handlePortfolioSummary(w http.ResponseWriter, r *http.Request) {
	tenantID := middleware.TenantIDFromContext(r.Context())
	accountID := chi.URLParam(r, "accountId")

	exists, err := s.repo.AccountExists(r.Context(), tenantID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check account")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "account not found")
		return
	}

	summary, err := s.repo.GetPortfolioSummary(r.Context(), tenantID, accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get portfolio summary")
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleListPositions(w http.ResponseWriter, r *http.Request) {
	tenantID := middleware.TenantIDFromContext(r.Context())
	accountID := chi.URLParam(r, "accountId")
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}

	// Validate status
	if status != "open" && status != "closed" && status != "all" {
		writeError(w, http.StatusBadRequest, "invalid status: must be open, closed, or all")
		return
	}

	positions, err := s.repo.ListPositions(r.Context(), tenantID, accountID, status)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list positions")
		return
	}
	writeJSON(w, http.StatusOK, positions)
}

func (s *Server) handleListTrades(w http.ResponseWriter, r *http.Request) {
	tenantID := middleware.TenantIDFromContext(r.Context())
	accountID := chi.URLParam(r, "accountId")
	q := r.URL.Query()

	filter := store.TradeFilter{
		Symbol:     q.Get("symbol"),
		Side:       q.Get("side"),
		MarketType: q.Get("market_type"),
		Cursor:     q.Get("cursor"),
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = limit
	}

	if startStr := q.Get("start"); startStr != "" {
		t, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start time")
			return
		}
		filter.Start = &t
	}

	if endStr := q.Get("end"); endStr != "" {
		t, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid end time")
			return
		}
		filter.End = &t
	}

	result, err := s.repo.ListTrades(r.Context(), tenantID, accountID, filter)
	if err != nil {
		if strings.Contains(err.Error(), "invalid cursor") {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list trades")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	tenantID := middleware.TenantIDFromContext(r.Context())
	accountID := chi.URLParam(r, "accountId")
	q := r.URL.Query()

	filter := store.OrderFilter{
		Status: q.Get("status"),
		Symbol: q.Get("symbol"),
		Cursor: q.Get("cursor"),
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		filter.Limit = limit
	}

	result, err := s.repo.ListOrders(r.Context(), tenantID, accountID, filter)
	if err != nil {
		if strings.Contains(err.Error(), "invalid cursor") {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to list orders")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
