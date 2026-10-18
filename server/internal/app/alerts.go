package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"sync-win/server/internal/logging"
	"sync-win/server/internal/store"
)

// Alert acknowledgements.
//
// The alert list itself is derived in the dashboard from lib/insights.ts, which
// is also what the meters and the Home cards read. Re-deriving the same rules
// in Go would give the server a second set of thresholds that could silently
// disagree with the screen, so the server deliberately stores only what the
// client cannot: which findings the operator dismissed. That state is scoped to
// the owner and therefore survives a reload and follows the account to another
// browser.

type alertAcksResponse struct {
	Acks []store.AlertAck `json:"acks"`
}

type alertAckRequest struct {
	ID string `json:"id"`
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := s.requireSession(w, r)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodGet:
		acks, err := s.store.ListAlertAcks(ownerID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(alertAcksResponse{Acks: acks})

	case http.MethodPost:
		var req alertAckRequest
		if !s.decodeBody(w, r, &req) {
			return
		}
		ack, err := s.store.AckAlert(ownerID, req.ID)
		if err != nil {
			s.writeAlertError(w, err)
			return
		}
		s.log.Audit(logging.CatDevice, logging.EventDeviceUpdated, "alert acknowledged", map[string]any{
			"alert_id": ack.ID,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ack)

	case http.MethodDelete:
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if id == "" {
			cleared, err := s.store.ClearAlertAcks(ownerID)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, err)
				return
			}
			s.log.Audit(logging.CatDevice, logging.EventDeviceUpdated, "alert acknowledgements cleared", map[string]any{
				"cleared": cleared,
			})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]int64{"cleared": cleared})
			return
		}
		if err := s.store.UnackAlert(ownerID, id); err != nil {
			s.writeAlertError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) writeAlertError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrAlertIDRequired) ||
		errors.Is(err, store.ErrAlertIDTooLong) ||
		errors.Is(err, store.ErrAlertIDInvalid) {
		s.writeError(w, http.StatusBadRequest, err)
		return
	}
	s.writeError(w, http.StatusInternalServerError, err)
}
