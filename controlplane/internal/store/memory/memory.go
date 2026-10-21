// Package memory adalah implementasi Store di memori.
//
// Dipakai untuk unit test dan untuk menjalankan API tanpa Postgres.
// Tidak persisten dan tidak aman untuk multi-proses.
package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/store"
)

// Store adalah implementasi in-memory dari store.Store.
type Store struct {
	mu          sync.Mutex
	tenants     map[string]domain.Tenant
	connections map[string]domain.Connection
	pipelines   map[string]domain.Pipeline
	runs        map[string]domain.Run
	events      map[string][]domain.RunEvent
	idempotency map[string]string // idempotency key -> run id
}

// New membuat Store kosong.
func New() *Store {
	return &Store{
		tenants:     map[string]domain.Tenant{},
		connections: map[string]domain.Connection{},
		pipelines:   map[string]domain.Pipeline{},
		runs:        map[string]domain.Run{},
		events:      map[string][]domain.RunEvent{},
		idempotency: map[string]string{},
	}
}

// NewID membuat UUID v4 tanpa dependency eksternal.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand tidak gagal dalam praktik; fallback berbasis waktu
		// tetap unik untuk satu proses.
		now := time.Now().UnixNano()
		return fmt.Sprintf("fallback-%016x", now)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // versi 4
	b[8] = (b[8] & 0x3f) | 0x80 // varian RFC 4122
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

func (s *Store) CreateTenant(_ context.Context, t domain.Tenant) (domain.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.tenants {
		if existing.Name == t.Name {
			return domain.Tenant{}, fmt.Errorf("%w: tenant %q sudah ada", domain.ErrConflict, t.Name)
		}
	}
	if t.ID == "" {
		t.ID = NewID()
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now().UTC()
	}
	s.tenants[t.ID] = t
	return t, nil
}

func (s *Store) GetTenant(_ context.Context, id string) (domain.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tenants[id]
	if !ok {
		return domain.Tenant{}, fmt.Errorf("%w: tenant %s", domain.ErrNotFound, id)
	}
	return t, nil
}

func (s *Store) ListTenants(_ context.Context) ([]domain.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) UpdateTenant(_ context.Context, t domain.Tenant) (domain.Tenant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.tenants[t.ID]
	if !ok {
		return domain.Tenant{}, fmt.Errorf("%w: tenant %s", domain.ErrNotFound, t.ID)
	}
	for id, other := range s.tenants {
		if id != t.ID && other.Name == t.Name {
			return domain.Tenant{}, fmt.Errorf("%w: tenant %q sudah ada", domain.ErrConflict, t.Name)
		}
	}
	if existing.CreatedAt.IsZero() {
		existing.CreatedAt = time.Now().UTC()
	}
	t.CreatedAt = existing.CreatedAt
	s.tenants[t.ID] = t
	return t, nil
}

func (s *Store) DeleteTenant(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[id]; !ok {
		return fmt.Errorf("%w: tenant %s", domain.ErrNotFound, id)
	}
	pipelineIDs := map[string]bool{}
	for pipelineID, pipeline := range s.pipelines {
		if pipeline.TenantID == id {
			pipelineIDs[pipelineID] = true
		}
	}
	for runID, run := range s.runs {
		if pipelineIDs[run.PipelineID] && run.Status.IsActive() {
			return fmt.Errorf("%w: tenant %s masih punya run aktif", domain.ErrConflict, id)
		}
		if pipelineIDs[run.PipelineID] {
			delete(s.events, runID)
			delete(s.runs, runID)
			for key, mappedRunID := range s.idempotency {
				if mappedRunID == runID {
					delete(s.idempotency, key)
				}
			}
		}
	}
	for pipelineID := range pipelineIDs {
		delete(s.pipelines, pipelineID)
	}
	for connectionID, connection := range s.connections {
		if connection.TenantID == id {
			delete(s.connections, connectionID)
		}
	}
	delete(s.tenants, id)
	return nil
}

func (s *Store) CreateConnection(_ context.Context, c domain.Connection) (domain.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[c.TenantID]; !ok {
		return domain.Connection{}, fmt.Errorf("%w: tenant %s", domain.ErrNotFound, c.TenantID)
	}
	for _, existing := range s.connections {
		if existing.TenantID == c.TenantID && existing.Name == c.Name {
			return domain.Connection{}, fmt.Errorf("%w: koneksi %q sudah ada", domain.ErrConflict, c.Name)
		}
	}
	if c.Driver.RequiresDatabaseSecret() && c.SecretRef == "" && c.Password == "" {
		return domain.Connection{}, fmt.Errorf("%w: secret_ref wajib diisi", domain.ErrInvalid)
	}
	if c.ID == "" {
		c.ID = NewID()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	s.connections[c.ID] = c
	return c, nil
}

func (s *Store) GetConnection(_ context.Context, id string) (domain.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.connections[id]
	if !ok {
		return domain.Connection{}, fmt.Errorf("%w: koneksi %s", domain.ErrNotFound, id)
	}
	return c, nil
}

func (s *Store) ListConnections(_ context.Context, tenantID string) ([]domain.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Connection, 0)
	for _, c := range s.connections {
		if tenantID == "" || c.TenantID == tenantID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) UpdateConnection(_ context.Context, c domain.Connection) (domain.Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.connections[c.ID]
	if !ok {
		return domain.Connection{}, fmt.Errorf("%w: koneksi %s", domain.ErrNotFound, c.ID)
	}
	if _, ok := s.tenants[c.TenantID]; !ok {
		return domain.Connection{}, fmt.Errorf("%w: tenant %s", domain.ErrNotFound, c.TenantID)
	}
	if c.Driver.RequiresDatabaseSecret() && c.SecretRef == "" && c.Password == "" {
		return domain.Connection{}, fmt.Errorf("%w: secret_ref wajib diisi", domain.ErrInvalid)
	}
	for id, other := range s.connections {
		if id != c.ID && other.TenantID == c.TenantID && other.Name == c.Name {
			return domain.Connection{}, fmt.Errorf("%w: koneksi %q sudah ada", domain.ErrConflict, c.Name)
		}
	}
	c.CreatedAt = existing.CreatedAt
	s.connections[c.ID] = c
	return c, nil
}

func (s *Store) DeleteConnection(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.connections[id]; !ok {
		return fmt.Errorf("%w: koneksi %s", domain.ErrNotFound, id)
	}
	for _, pipeline := range s.pipelines {
		if pipeline.SourceConnectionID == id || pipeline.DestinationConnectionID == id {
			return fmt.Errorf("%w: koneksi %s masih dipakai pipeline %s",
				domain.ErrConflict, id, pipeline.ID)
		}
	}
	delete(s.connections, id)
	return nil
}

func (s *Store) CreatePipeline(_ context.Context, p domain.Pipeline) (domain.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tenants[p.TenantID]; !ok {
		return domain.Pipeline{}, fmt.Errorf("%w: tenant %s", domain.ErrNotFound, p.TenantID)
	}
	// Aturan yang sama dengan CHECK constraint di migrations/0001_init.sql.
	if p.StateIDSuffix != nil && containsColon(*p.StateIDSuffix) {
		return domain.Pipeline{}, fmt.Errorf(
			"%w: state_id_suffix tidak boleh mengandung ':'", domain.ErrInvalid)
	}
	for _, existing := range s.pipelines {
		if existing.TenantID == p.TenantID && existing.Name == p.Name {
			return domain.Pipeline{}, fmt.Errorf("%w: pipeline %q sudah ada", domain.ErrConflict, p.Name)
		}
	}
	if p.ID == "" {
		p.ID = NewID()
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	if p.DefaultTimeoutSeconds == 0 {
		p.DefaultTimeoutSeconds = 3600
	}
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 3
	}
	s.pipelines[p.ID] = p
	return p, nil
}

func (s *Store) GetPipeline(_ context.Context, id string) (domain.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pipelines[id]
	if !ok {
		return domain.Pipeline{}, fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, id)
	}
	return p, nil
}

func (s *Store) ListPipelines(_ context.Context, tenantID string) ([]domain.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Pipeline, 0)
	for _, p := range s.pipelines {
		if tenantID == "" || p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) UpdatePipeline(_ context.Context, p domain.Pipeline) (domain.Pipeline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.pipelines[p.ID]
	if !ok {
		return domain.Pipeline{}, fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, p.ID)
	}
	if _, ok := s.tenants[p.TenantID]; !ok {
		return domain.Pipeline{}, fmt.Errorf("%w: tenant %s", domain.ErrNotFound, p.TenantID)
	}
	if p.StateIDSuffix != nil && containsColon(*p.StateIDSuffix) {
		return domain.Pipeline{}, fmt.Errorf(
			"%w: state_id_suffix tidak boleh mengandung ':'", domain.ErrInvalid)
	}
	for id, other := range s.pipelines {
		if id != p.ID && other.TenantID == p.TenantID && other.Name == p.Name {
			return domain.Pipeline{}, fmt.Errorf("%w: pipeline %q sudah ada", domain.ErrConflict, p.Name)
		}
	}
	for _, run := range s.runs {
		if run.PipelineID == p.ID && run.Status.IsActive() {
			return domain.Pipeline{}, fmt.Errorf("%w: pipeline %s masih punya run aktif",
				domain.ErrConflict, p.ID)
		}
	}
	p.CreatedAt = existing.CreatedAt
	p.UpdatedAt = time.Now().UTC()
	s.pipelines[p.ID] = p
	return p, nil
}

func (s *Store) DeletePipeline(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.pipelines[id]; !ok {
		return fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, id)
	}
	for _, r := range s.runs {
		if r.PipelineID == id && r.Status.IsActive() {
			return fmt.Errorf("%w: pipeline %s masih punya run aktif", domain.ErrConflict, id)
		}
	}
	delete(s.pipelines, id)
	return nil
}

func (s *Store) CreateRun(_ context.Context, run domain.Run, idempotencyKey string) (domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if idempotencyKey != "" {
		if existingID, ok := s.idempotency[idempotencyKey]; ok {
			return s.runs[existingID], nil
		}
	}
	pipe, ok := s.pipelines[run.PipelineID]
	if !ok {
		return domain.Run{}, fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, run.PipelineID)
	}
	if !pipe.Enabled {
		return domain.Run{}, fmt.Errorf("%w: pipeline %s tidak aktif", domain.ErrInvalid, pipe.ID)
	}
	for _, existing := range s.runs {
		if existing.PipelineID == run.PipelineID && existing.Status.IsActive() {
			return domain.Run{}, fmt.Errorf(
				"%w: pipeline %s sudah punya run aktif %s (%s)",
				domain.ErrConflict, run.PipelineID, existing.ID, existing.Status)
		}
	}
	if run.ID == "" {
		run.ID = NewID()
	}
	if run.Status == "" {
		run.Status = domain.StatusQueued
	}
	if run.Attempt == 0 {
		run.Attempt = 1
	}
	if run.Trigger == "" {
		run.Trigger = "manual"
	}
	if run.TimeoutSeconds == 0 {
		run.TimeoutSeconds = pipe.DefaultTimeoutSeconds
	}
	run.CreatedAt = time.Now().UTC()
	s.runs[run.ID] = run
	if idempotencyKey != "" {
		s.idempotency[idempotencyKey] = run.ID
	}
	return run, nil
}

func (s *Store) GetRun(_ context.Context, id string) (domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return domain.Run{}, fmt.Errorf("%w: run %s", domain.ErrNotFound, id)
	}
	return r, nil
}

func (s *Store) ListRuns(_ context.Context, filter store.RunFilter) ([]domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Run, 0)
	for _, r := range s.runs {
		if filter.PipelineID != "" && r.PipelineID != filter.PipelineID {
			continue
		}
		if len(filter.Statuses) > 0 {
			found := false
			for _, st := range filter.Statuses {
				if r.Status == st {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *Store) ClaimRun(_ context.Context, workerID string, lease time.Duration) (*domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	candidates := make([]domain.Run, 0)
	for _, r := range s.runs {
		if r.Status == domain.StatusQueued {
			candidates = append(candidates, r)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].CreatedAt.Before(candidates[j].CreatedAt) })

	run := candidates[0]
	now := time.Now().UTC()
	expires := now.Add(lease)
	run.Status = domain.StatusRunning
	run.ClaimedBy = &workerID
	run.LeaseExpiresAt = &expires
	if run.StartedAt == nil {
		run.StartedAt = &now
	}
	s.runs[run.ID] = run
	return &run, nil
}

func (s *Store) CompleteRun(_ context.Context, id string, c store.Completion) (domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return domain.Run{}, fmt.Errorf("%w: run %s", domain.ErrNotFound, id)
	}
	now := time.Now().UTC()
	r.Status = c.Status
	r.ExitCode = c.ExitCode
	r.Error = c.Error
	r.LogTail = c.LogTail
	if c.MeltanoStateID != "" {
		r.MeltanoStateID = c.MeltanoStateID
	}
	if c.MeltanoRunID != "" {
		r.MeltanoRunID = c.MeltanoRunID
	}
	if c.AutoAttempts > 0 {
		r.AutoAttempts = c.AutoAttempts
	}
	r.EndedAt = &now
	s.runs[id] = r
	return r, nil
}

func (s *Store) HeartbeatRun(_ context.Context, id, workerID string, lease time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return fmt.Errorf("%w: run %s", domain.ErrNotFound, id)
	}
	if r.ClaimedBy == nil || *r.ClaimedBy != workerID {
		return fmt.Errorf("%w: run %s bukan milik worker %s", domain.ErrConflict, id, workerID)
	}
	if r.Status.IsTerminal() {
		return fmt.Errorf("%w: run %s sudah selesai", domain.ErrConflict, id)
	}
	expires := time.Now().UTC().Add(lease)
	r.LeaseExpiresAt = &expires
	s.runs[id] = r
	return nil
}

func (s *Store) RequestCancel(_ context.Context, id string) (domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return domain.Run{}, fmt.Errorf("%w: run %s", domain.ErrNotFound, id)
	}
	if r.Status.IsTerminal() {
		return r, nil // idempoten
	}
	if r.Status == domain.StatusQueued {
		now := time.Now().UTC()
		r.Status = domain.StatusCancelled
		r.EndedAt = &now
	} else {
		r.Status = domain.StatusCancelling
	}
	s.runs[id] = r
	return r, nil
}

func (s *Store) ReapExpired(_ context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for id, r := range s.runs {
		if r.Status != domain.StatusRunning && r.Status != domain.StatusCancelling {
			continue
		}
		if r.LeaseExpiresAt == nil || r.LeaseExpiresAt.After(now) {
			continue
		}
		r.Status = domain.StatusFailed
		r.Error = "lease kedaluwarsa; worker dianggap mati sebelum melaporkan hasil"
		r.EndedAt = &now
		s.runs[id] = r
		n++
	}
	return n, nil
}

func (s *Store) AppendEvent(_ context.Context, ev domain.RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[ev.RunID]; !ok {
		return fmt.Errorf("%w: run %s", domain.ErrNotFound, ev.RunID)
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	s.events[ev.RunID] = append(s.events[ev.RunID], ev)
	return nil
}

func (s *Store) ListEvents(_ context.Context, runID string, limit int) ([]domain.RunEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.events[runID]
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	out := make([]domain.RunEvent, len(all))
	copy(out, all)
	return out, nil
}

func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}
