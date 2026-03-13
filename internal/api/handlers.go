package api

import (
	"net/http"

	"github.com/Signal-ngn/trader/internal/api/middleware"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleAuthResolve returns the resolved tenant ID for the authenticated caller.
func (s *Server) handleAuthResolve(w http.ResponseWriter, r *http.Request) {
	tenantID := middleware.TenantIDFromContext(r.Context())
	writeJSON(w, http.StatusOK, map[string]string{
		"tenant_id": tenantID.String(),
	})
}
