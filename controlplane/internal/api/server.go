// Package api menyediakan HTTP API control plane.
//
// Cakupan implementasi saat ini adalah subset dari
// meltano-go-reference/14-api-reference.md: tenant, connection, pipeline,
// run, event, dan meta. Endpoint yang belum ada tercantum di README.
package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/meltano"
	"github.com/example/controlplane/internal/store"
)

// Version adalah versi control plane yang dilaporkan /v1/version.
const Version = "0.1.0"

// Server merangkai handler HTTP.
type Server struct {
	store          store.Store
	log            *slog.Logger
	meltanoVersion string
}

// New membuat Server.
func New(st store.Store, log *slog.Logger, meltanoVersion string) *Server {
	return &Server{store: st, log: log, meltanoVersion: meltanoVersion}
}

// Handler mengembalikan router dengan seluruh route terdaftar.
//
// Pola routing memakai method + wildcard bawaan net/http (Go 1.22+),
// sehingga tidak diperlukan router eksternal.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /readyz", s.handleReadyz)
	mux.HandleFunc("GET /v1/version", s.handleVersion)

	mux.HandleFunc("GET /v1/tenants", s.handleListTenants)
	mux.HandleFunc("POST /v1/tenants", s.handleCreateTenant)

	mux.HandleFunc("GET /v1/connections", s.handleListConnections)
	mux.HandleFunc("POST /v1/connections", s.handleCreateConnection)

	mux.HandleFunc("GET /v1/pipelines", s.handleListPipelines)
	mux.HandleFunc("POST /v1/pipelines", s.handleCreatePipeline)
	mux.HandleFunc("GET /v1/pipelines/{pipelineID}", s.handleGetPipeline)
	mux.HandleFunc("DELETE /v1/pipelines/{pipelineID}", s.handleDeletePipeline)
	mux.HandleFunc("POST /v1/pipelines/{pipelineID}/runs", s.handleCreateRun)
	mux.HandleFunc("POST /v1/pipelines/{pipelineID}/validate", s.handleValidatePipeline)

	mux.HandleFunc("GET /v1/runs", s.handleListRuns)
	mux.HandleFunc("GET /v1/runs/{runID}", s.handleGetRun)
	mux.HandleFunc("POST /v1/runs/{runID}/cancel", s.handleCancelRun)
	mux.HandleFunc("POST /v1/runs/{runID}/retry", s.handleRetryRun)
	mux.HandleFunc("GET /v1/runs/{runID}/events", s.handleListEvents)

	return s.withRequestID(s.withLogging(mux))
}

// --- middleware ---

// withRequestID meneruskan atau membuat X-Request-Id.
func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("http",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Duration("duration", time.Since(start)))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// --- meta ---

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	// Store in-memory tidak punya ping; kehadiran store dianggap siap.
	if _, err := s.store.ListTenants(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ready": false,
			"checks": map[string]any{
				"job_store": map[string]any{"ok": false, "error": err.Error()},
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ready": true,
		"checks": map[string]any{
			"job_store": map[string]any{"ok": true},
		},
	})
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"control_plane": Version,
		"meltano":       s.meltanoVersion,
		"api_version":   "v1",
	})
}

// --- tenant ---

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListTenants(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false})
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name            string `json:"name"`
		DisplayName     string `json:"display_name"`
		EnvironmentName string `json:"environment_name"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("name wajib diisi")))
		return
	}
	if in.EnvironmentName == "" {
		// Environment menentukan komponen pertama state_id, jadi harus eksplisit.
		in.EnvironmentName = in.Name
	}
	t, err := s.store.CreateTenant(r.Context(), domain.Tenant{
		Name:            in.Name,
		DisplayName:     in.DisplayName,
		EnvironmentName: in.EnvironmentName,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

// --- connection ---

func (s *Server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListConnections(r.Context(), r.URL.Query().Get("tenant_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	// Nilai secret tidak pernah dikembalikan; secret_ref sudah berupa referensi.
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false})
}

func (s *Server) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TenantID  string         `json:"tenant_id"`
		Name      string         `json:"name"`
		Kind      string         `json:"kind"`
		Driver    string         `json:"driver"`
		Endpoint  map[string]any `json:"endpoint"`
		Username  string         `json:"username"`
		SecretRef string         `json:"secret_ref"`
		Options   map[string]any `json:"options"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.SecretRef) == "" {
		// Kredensial tidak boleh dikirim ke API; hanya referensinya.
		writeError(w, r, errors.Join(domain.ErrInvalid,
			errors.New("secret_ref wajib diisi; nilai kredensial tidak boleh dikirim ke API")))
		return
	}
	kind := domain.ConnectionKind(in.Kind)
	if kind != domain.KindSource && kind != domain.KindDestination {
		writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("kind harus source atau destination")))
		return
	}
	c, err := s.store.CreateConnection(r.Context(), domain.Connection{
		TenantID:  in.TenantID,
		Name:      in.Name,
		Kind:      kind,
		Driver:    domain.Driver(in.Driver),
		Endpoint:  in.Endpoint,
		Username:  in.Username,
		SecretRef: in.SecretRef,
		Options:   in.Options,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// --- pipeline ---

func (s *Server) handleListPipelines(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListPipelines(r.Context(), r.URL.Query().Get("tenant_id"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false})
}

func (s *Server) handleCreatePipeline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		TenantID                string  `json:"tenant_id"`
		Name                    string  `json:"name"`
		Extractor               string  `json:"extractor"`
		Loader                  string  `json:"loader"`
		SourceConnectionID      string  `json:"source_connection_id"`
		DestinationConnectionID string  `json:"destination_connection_id"`
		StateIDSuffix           *string `json:"state_id_suffix"`
		DefaultTimeoutSeconds   int     `json:"default_timeout_seconds"`
		MaxAttempts             int     `json:"max_attempts"`
		Enabled                 *bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	p, err := s.store.CreatePipeline(r.Context(), domain.Pipeline{
		TenantID:                in.TenantID,
		Name:                    in.Name,
		Extractor:               in.Extractor,
		Loader:                  in.Loader,
		SourceConnectionID:      in.SourceConnectionID,
		DestinationConnectionID: in.DestinationConnectionID,
		StateIDSuffix:           in.StateIDSuffix,
		DefaultTimeoutSeconds:   in.DefaultTimeoutSeconds,
		MaxAttempts:             in.MaxAttempts,
		Enabled:                 enabled,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetPipeline(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPipeline(r.Context(), r.PathValue("pipelineID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePipeline(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeletePipeline(r.Context(), r.PathValue("pipelineID")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleValidatePipeline menghitung state_id dan memeriksa prasyarat tanpa
// menjalankan apa pun. Pemetaan penuhnya ke `meltano run --dry-run` belum
// diimplementasikan (lihat README).
func (s *Server) handleValidatePipeline(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.GetPipeline(r.Context(), r.PathValue("pipelineID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	t, err := s.store.GetTenant(r.Context(), p.TenantID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	suffix := ""
	if p.StateIDSuffix != nil {
		suffix = *p.StateIDSuffix
	}
	stateID, err := meltano.StateID(t.EnvironmentName, p.Extractor, p.Loader, suffix)
	checks := []map[string]any{
		{"name": "extractor_present", "ok": p.Extractor != ""},
		{"name": "loader_present", "ok": p.Loader != ""},
		{"name": "state_id_suffix_charset", "ok": err == nil},
		{"name": "environment_present", "ok": t.EnvironmentName != ""},
	}
	resp := map[string]any{"valid": err == nil, "checks": checks}
	if err == nil {
		resp["resolved_state_id"] = stateID
	}
	status := http.StatusOK
	if err != nil {
		status = http.StatusUnprocessableEntity
		resp["valid"] = false
		resp["detail"] = err.Error()
	}
	writeJSON(w, status, resp)
}

// --- run ---

func (s *Server) handleCreateRun(w http.ResponseWriter, r *http.Request) {
	pipelineID := r.PathValue("pipelineID")
	p, err := s.store.GetPipeline(r.Context(), pipelineID)
	if err != nil {
		writeError(w, r, err)
		return
	}

	var in struct {
		FullRefresh    bool    `json:"full_refresh"`
		RefreshCatalog bool    `json:"refresh_catalog"`
		StateIDSuffix  *string `json:"state_id_suffix"`
		TimeoutSeconds int     `json:"timeout_seconds"`
		Trigger        string  `json:"trigger"`
		Reason         string  `json:"reason"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &in) {
		return
	}
	if in.TimeoutSeconds == 0 {
		in.TimeoutSeconds = p.DefaultTimeoutSeconds
	}
	if in.Trigger == "" {
		in.Trigger = "manual"
	}
	// Field `force` sengaja tidak didukung: menonaktifkan satu-satunya pengaman
	// yang menggantikan advisory check Meltano (doc 14 bagian 14.8).
	suffix := p.StateIDSuffix
	if in.StateIDSuffix != nil {
		suffix = in.StateIDSuffix
	}
	run, err := s.store.CreateRun(r.Context(), domain.Run{
		PipelineID:     pipelineID,
		Status:         domain.StatusQueued,
		Attempt:        1,
		Trigger:        in.Trigger,
		Reason:         in.Reason,
		TimeoutSeconds: in.TimeoutSeconds,
		FullRefresh:    in.FullRefresh,
		StateIDSuffix:  suffix,
	}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/runs/"+run.ID)
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	var statuses []domain.RunStatus
	for _, raw := range r.URL.Query()["status"] {
		st := domain.RunStatus(raw)
		if !st.Valid() {
			writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("status tidak dikenal: "+raw)))
			return
		}
		statuses = append(statuses, st)
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 200 {
			writeError(w, r, errors.Join(domain.ErrInvalid, errors.New("limit harus 1..200")))
			return
		}
		limit = n
	}
	items, err := s.store.ListRuns(r.Context(), store.RunFilter{
		PipelineID: r.URL.Query().Get("pipeline_id"),
		Statuses:   statuses,
		Limit:      limit,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "has_more": false})
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.GetRun(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	run, err := s.store.RequestCancel(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	now := time.Now().UTC()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":              run.Status,
		"cancel_requested_at": now,
	})
}

func (s *Server) handleRetryRun(w http.ResponseWriter, r *http.Request) {
	prev, err := s.store.GetRun(r.Context(), r.PathValue("runID"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	if !prev.Status.IsTerminal() {
		writeError(w, r, errors.Join(domain.ErrConflict,
			errors.New("run belum selesai; batalkan dulu sebelum retry")))
		return
	}
	id := prev.ID
	run, err := s.store.CreateRun(r.Context(), domain.Run{
		PipelineID:     prev.PipelineID,
		Status:         domain.StatusQueued,
		Attempt:        1,
		RetriedFrom:    &id,
		Trigger:        "retry",
		TimeoutSeconds: prev.TimeoutSeconds,
		FullRefresh:    prev.FullRefresh,
		StateIDSuffix:  prev.StateIDSuffix,
	}, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, run)
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.ListEvents(r.Context(), r.PathValue("runID"), 200)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": events, "has_more": false})
}
