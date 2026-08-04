package runtime

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a448582655/vibe-proxy/internal/telemetry"
)

func (s *Server) adminObservabilityRequests(w http.ResponseWriter, r *http.Request) {
	setObservabilityResponseHeaders(w)
	if !s.adminAuthorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.observability == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "observability store is unavailable")
		return
	}
	query, err := parseRequestQuery(r)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.observability.QueryRequests(query)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, page)
}

func (s *Server) adminObservabilityRequest(w http.ResponseWriter, r *http.Request) {
	setObservabilityResponseHeaders(w)
	if !s.adminAuthorize(w, r) {
		return
	}
	if s.observability == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "observability store is unavailable")
		return
	}
	remainder := strings.TrimPrefix(r.URL.Path, "/admin/observability/requests/")
	parts := strings.Split(strings.Trim(remainder, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		s.writeJSONError(w, http.StatusNotFound, "request not found")
		return
	}
	requestID, err := url.PathUnescape(parts[0])
	if err != nil || requestID == "" {
		s.writeJSONError(w, http.StatusBadRequest, "invalid request id")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		details, err := s.observability.RequestDetails(requestID)
		s.writeObservabilityResult(w, details, err)
		return
	}
	if len(parts) == 2 && parts[1] == "diff" && r.Method == http.MethodGet {
		diff, err := s.observability.RequestDiff(requestID, strings.TrimSpace(r.URL.Query().Get("base_id")))
		s.writeObservabilityResult(w, diff, err)
		return
	}
	if len(parts) == 2 && parts[1] == "content" && r.Method == http.MethodDelete {
		manager, ok := s.observability.(interface {
			DeleteRequestContent(string) (bool, error)
		})
		if !ok {
			s.writeJSONError(w, http.StatusNotImplemented, "content deletion is unavailable")
			return
		}
		deleted, err := manager.DeleteRequestContent(requestID)
		if err != nil {
			s.writeObservabilityResult(w, nil, err)
			return
		}
		s.writeJSON(w, map[string]any{"request_id": requestID, "deleted": deleted, "state": telemetry.CaptureStatusExpired})
		return
	}
	s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) adminObservabilitySessions(w http.ResponseWriter, r *http.Request) {
	setObservabilityResponseHeaders(w)
	if !s.adminAuthorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.observability == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "observability store is unavailable")
		return
	}
	query, err := parseSessionQuery(r)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.observability.QuerySessions(query)
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, page)
}

func (s *Server) adminObservabilitySession(w http.ResponseWriter, r *http.Request) {
	setObservabilityResponseHeaders(w)
	if !s.adminAuthorize(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		s.writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.observability == nil {
		s.writeJSONError(w, http.StatusServiceUnavailable, "observability store is unavailable")
		return
	}
	rawSessionID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/observability/sessions/"), "/")
	sessionID, err := url.PathUnescape(rawSessionID)
	if err != nil || sessionID == "" {
		s.writeJSONError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	page, err := s.observability.QuerySessions(telemetry.SessionQuery{Limit: 1, SessionID: sessionID})
	if err != nil {
		s.writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(page.Items) == 0 {
		s.writeJSONError(w, http.StatusNotFound, "session not found")
		return
	}
	requests, err := s.observability.SessionRequests(sessionID)
	if err != nil {
		s.writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, map[string]any{"session": page.Items[0], "requests": requests})
}

func parseRequestQuery(r *http.Request) (telemetry.RequestQuery, error) {
	values := r.URL.Query()
	limit, err := parseOptionalInt(values.Get("limit"))
	if err != nil || values.Get("limit") != "" && (limit < 1 || limit > 200) {
		return telemetry.RequestQuery{}, errors.New("limit must be between 1 and 200")
	}
	statusClass, err := parseOptionalInt(values.Get("status_class"))
	if err != nil || values.Get("status_class") != "" && (statusClass < 1 || statusClass > 5) {
		return telemetry.RequestQuery{}, errors.New("status_class must be between 1 and 5")
	}
	from, err := parseOptionalTime(values.Get("from"))
	if err != nil {
		return telemetry.RequestQuery{}, errors.New("from must be RFC3339")
	}
	to, err := parseOptionalTime(values.Get("to"))
	if err != nil {
		return telemetry.RequestQuery{}, errors.New("to must be RFC3339")
	}
	if from != nil && to != nil && from.After(*to) {
		return telemetry.RequestQuery{}, errors.New("from must not be after to")
	}
	if captureStatus := values.Get("capture_status"); captureStatus != "" && !validCaptureStatus(captureStatus) {
		return telemetry.RequestQuery{}, errors.New("invalid capture_status")
	}
	return telemetry.RequestQuery{
		Limit: limit, Cursor: values.Get("cursor"), AgentID: values.Get("agent_id"),
		PrincipalName: values.Get("principal_name"), SessionID: values.Get("session_id"),
		ProjectID: values.Get("project_id"), Model: values.Get("model"), Provider: values.Get("provider"),
		Protocol: values.Get("protocol"), StatusClass: statusClass, CaptureStatus: values.Get("capture_status"),
		Query: values.Get("q"), From: from, To: to,
	}, nil
}

func parseSessionQuery(r *http.Request) (telemetry.SessionQuery, error) {
	values := r.URL.Query()
	limit, err := parseOptionalInt(values.Get("limit"))
	if err != nil || values.Get("limit") != "" && (limit < 1 || limit > 200) {
		return telemetry.SessionQuery{}, errors.New("limit must be between 1 and 200")
	}
	return telemetry.SessionQuery{
		Limit: limit, Cursor: values.Get("cursor"), SessionID: values.Get("session_id"),
		AgentID: values.Get("agent_id"), PrincipalName: values.Get("principal_name"),
		ProjectID: values.Get("project_id"), Query: values.Get("q"),
	}, nil
}

func validCaptureStatus(status string) bool {
	switch status {
	case telemetry.CaptureStatusNotCaptured, telemetry.CaptureStatusCaptured, telemetry.CaptureStatusRedacted,
		telemetry.CaptureStatusTruncated, telemetry.CaptureStatusExpired, telemetry.CaptureStatusDropped:
		return true
	default:
		return false
	}
}

func parseOptionalInt(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return strconv.Atoi(value)
}

func parseOptionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func setObservabilityResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func (s *Server) writeObservabilityResult(w http.ResponseWriter, value any, err error) {
	if err == nil {
		s.writeJSON(w, value)
		return
	}
	if errors.Is(err, telemetry.ErrRequestNotFound) {
		s.writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeJSONError(w, http.StatusInternalServerError, err.Error())
}
