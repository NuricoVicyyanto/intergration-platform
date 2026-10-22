// Package postgres adalah implementasi store.Store di atas PostgreSQL.
//
// Klaim job memakai `UPDATE ... WHERE id = (SELECT ... FOR UPDATE SKIP LOCKED)`
// seperti yang sudah disiapkan di migrations/0001_init.sql, sehingga beberapa
// worker bisa menarik dari antrean yang sama tanpa saling memblokir
// (meltano-go-reference/13 bagian 13.10).
//
// Semua query memakai protokol sederhana pgx (parameter di-inline sebagai
// literal) supaya file migrasi yang memuat banyak statement bisa dieksekusi apa
// adanya. Karena itu setiap parameter bertipe non-text selalu diberi cast
// eksplisit (::uuid, ::timestamptz, ::jsonb, ...).
package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/store"
	"github.com/example/controlplane/migrations"
)

// SQLSTATE dan potongan query yang dipakai berulang.
const (
	sqlstateInvalidText = "22P02" // uuid/format tidak valid
	sqlstateUnique      = "23505"
	sqlstateForeignKey  = "23503"
	sqlstateCheck       = "23514"

	migrationLockKey   = 75_341_590_050_010
	leaseExpiredReason = "lease kedaluwarsa; worker dianggap mati sebelum melaporkan hasil"
	activeRunStatesSQL = `('queued','running','cancelling')`
	terminalStatesSQL  = `('succeeded','failed','cancelled','timed_out','lost')`
)

// Store adalah implementasi store.Store di atas PostgreSQL.
type Store struct {
	db *sql.DB
}

// Open membuka pool koneksi ke Postgres dan memastikan koneksi hidup.
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("CP_DATABASE_URL tidak bisa dibaca: %w", err)
	}
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("koneksi Postgres gagal: %w", err)
	}
	return &Store{db: db}, nil
}

// NewWithDB membungkus *sql.DB yang sudah ada. Dipakai unit test.
func NewWithDB(db *sql.DB) *Store { return &Store{db: db} }

// Close menutup pool koneksi.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM auth_users`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	const q = `
		INSERT INTO auth_users
		  (id, username, display_name, password_hash, role, created_at, updated_at)
		VALUES
		  (COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()), lower($2), $3, $4, $5,
		   COALESCE($6::timestamptz, now()), COALESCE($7::timestamptz, now()))
		RETURNING id::text, username, display_name, role, password_hash, created_at, updated_at`
	out, err := scanUser(s.db.QueryRowContext(ctx, q,
		user.ID, user.Username, user.DisplayName, user.PasswordHash, user.Role,
		tsParam(user.CreatedAt), tsParam(user.UpdatedAt)))
	if err != nil {
		if pgErrCode(err) == sqlstateUnique {
			return domain.User{}, fmt.Errorf("%w: user %q sudah ada", domain.ErrConflict, user.Username)
		}
		return domain.User{}, err
	}
	return out, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (domain.User, error) {
	const q = `
		SELECT id::text, username, display_name, role, password_hash, created_at, updated_at
		  FROM auth_users WHERE username = lower($1)`
	user, err := scanUser(s.db.QueryRowContext(ctx, q, username))
	if err != nil {
		return domain.User{}, lookupErr(err, "user", username)
	}
	return user, nil
}

func (s *Store) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	const q = `
		SELECT id::text, username, display_name, role, password_hash, created_at, updated_at
		  FROM auth_users WHERE id = $1::uuid`
	user, err := scanUser(s.db.QueryRowContext(ctx, q, id))
	if err != nil {
		return domain.User{}, lookupErr(err, "user", id)
	}
	return user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	const q = `
		SELECT id::text, username, display_name, role, password_hash, created_at, updated_at
		  FROM auth_users ORDER BY username`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]domain.User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUser(
	ctx context.Context,
	user domain.User,
	passwordHash *string,
) (domain.User, error) {
	const q = `
		UPDATE auth_users
		   SET username = lower($2),
		       display_name = $3,
		       role = $4,
		       password_hash = COALESCE(NULLIF($5,''), password_hash),
		       updated_at = now()
		 WHERE id = $1::uuid
		RETURNING id::text, username, display_name, role, password_hash, created_at, updated_at`
	hash := ""
	if passwordHash != nil {
		hash = *passwordHash
	}
	out, err := scanUser(s.db.QueryRowContext(ctx, q,
		user.ID, user.Username, user.DisplayName, user.Role, hash))
	if err != nil {
		if pgErrCode(err) == sqlstateUnique {
			return domain.User{}, fmt.Errorf("%w: user %q sudah ada", domain.ErrConflict, user.Username)
		}
		return domain.User{}, lookupErr(err, "user", user.ID)
	}
	return out, nil
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM auth_users WHERE id = $1::uuid`, id)
	if err != nil {
		return lookupErr(err, "user", id)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: user %s", domain.ErrNotFound, id)
	}
	return nil
}

func (s *Store) CreateSession(
	ctx context.Context,
	tokenHash, userID string,
	expiresAt time.Time,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_sessions (token_hash, user_id, expires_at)
		VALUES ($1, $2::uuid, $3::timestamptz)`,
		tokenHash, userID, expiresAt.UTC())
	return err
}

func (s *Store) GetSessionUser(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (domain.User, error) {
	const q = `
		SELECT u.id::text, u.username, u.display_name, u.role, u.password_hash,
		       u.created_at, u.updated_at
		  FROM auth_sessions s
		  JOIN auth_users u ON u.id = s.user_id
		 WHERE s.token_hash = $1 AND s.expires_at > $2::timestamptz`
	user, err := scanUser(s.db.QueryRowContext(ctx, q, tokenHash, now.UTC()))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("%w: session tidak valid", domain.ErrUnauthorized)
	}
	if err != nil {
		return domain.User{}, err
	}
	return user, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_sessions WHERE user_id = $1::uuid`, userID)
	return err
}

// Migrate menjalankan seluruh file .sql di package migrations secara berurutan.
//
// Migrasi bersifat idempoten (IF NOT EXISTS) dan dilindungi advisory lock,
// sehingga API dan worker boleh sama-sama memanggilnya saat start.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("siapkan tabel schema_migrations: %w", err)
	}

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return fmt.Errorf("baca direktori migrasi: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		applied, err := s.migrationApplied(ctx, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return fmt.Errorf("baca migrasi %s: %w", name, err)
		}
		if err := s.applyMigration(ctx, name, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrationApplied(ctx context.Context, version string) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM schema_migrations WHERE version = $1`, version).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("cek migrasi %s: %w", version, err)
	default:
		return true, nil
	}
}

func (s *Store) applyMigration(ctx context.Context, version, body string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mulai transaksi migrasi %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Advisory lock transaksional: dua proses yang start bersamaan tidak
	// menjalankan migrasi yang sama dua kali.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, migrationLockKey); err != nil {
		return fmt.Errorf("ambil advisory lock migrasi: %w", err)
	}
	var one int
	err = tx.QueryRowContext(ctx,
		`SELECT 1 FROM schema_migrations WHERE version = $1`, version).Scan(&one)
	if err == nil {
		return tx.Commit() // sudah dijalankan proses lain
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("cek migrasi %s: %w", version, err)
	}

	if _, err := tx.ExecContext(ctx, body); err != nil {
		return fmt.Errorf("jalankan migrasi %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("catat migrasi %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrasi %s: %w", version, err)
	}
	return nil
}

// --- tenant ---

// CreateTenant menyimpan tenant baru. Nama tenant harus unik.
func (s *Store) CreateTenant(ctx context.Context, t domain.Tenant) (domain.Tenant, error) {
	const q = `
		INSERT INTO tenants (id, name, display_name, environment_name, project_dir, created_at)
		VALUES (COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()), $2, NULLIF($3,''), $4, $5,
		        COALESCE($6::timestamptz, now()))
		RETURNING id::text, name, COALESCE(display_name,''), environment_name, project_dir, created_at`

	out, err := scanTenant(s.db.QueryRowContext(ctx, q,
		t.ID, t.Name, t.DisplayName, t.EnvironmentName, t.ProjectDir, tsParam(t.CreatedAt)))
	if err != nil {
		if pgErrCode(err) == sqlstateUnique {
			return domain.Tenant{}, fmt.Errorf("%w: tenant %q sudah ada", domain.ErrConflict, t.Name)
		}
		return domain.Tenant{}, err
	}
	return out, nil
}

// GetTenant mengambil tenant berdasarkan id.
func (s *Store) GetTenant(ctx context.Context, id string) (domain.Tenant, error) {
	const q = `
		SELECT id::text, name, COALESCE(display_name,''), environment_name, project_dir, created_at
		  FROM tenants WHERE id = $1::uuid`
	t, err := scanTenant(s.db.QueryRowContext(ctx, q, id))
	if err != nil {
		return domain.Tenant{}, lookupErr(err, "tenant", id)
	}
	return t, nil
}

// ListTenants mengembalikan seluruh tenant, terlama lebih dulu.
func (s *Store) ListTenants(ctx context.Context) ([]domain.Tenant, error) {
	const q = `
		SELECT id::text, name, COALESCE(display_name,''), environment_name, project_dir, created_at
		  FROM tenants ORDER BY created_at, id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Tenant, 0)
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// UpdateTenant memperbarui metadata tenant. Direktori project tidak dipindahkan
// otomatis; caller harus memastikan ProjectDir tetap valid.
func (s *Store) UpdateTenant(ctx context.Context, t domain.Tenant) (domain.Tenant, error) {
	const q = `
		UPDATE tenants
		   SET name = $2, display_name = NULLIF($3,''), environment_name = $4,
		       project_dir = $5
		 WHERE id = $1::uuid
		RETURNING id::text, name, COALESCE(display_name,''), environment_name, project_dir, created_at`
	out, err := scanTenant(s.db.QueryRowContext(ctx, q,
		t.ID, t.Name, t.DisplayName, t.EnvironmentName, t.ProjectDir))
	if err != nil {
		if pgErrCode(err) == sqlstateUnique {
			return domain.Tenant{}, fmt.Errorf("%w: tenant %q sudah ada", domain.ErrConflict, t.Name)
		}
		return domain.Tenant{}, lookupErr(err, "tenant", t.ID)
	}
	return out, nil
}

// DeleteTenant menghapus tenant beserta koneksi/pipeline/run yang bergantung
// padanya. Penghapusan ditolak bila masih ada run aktif.
func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	const q = `
		DELETE FROM tenants t
		 WHERE t.id = $1::uuid
		   AND NOT EXISTS (
		       SELECT 1
		         FROM runs r
		         JOIN pipelines p ON p.id = r.pipeline_id
		        WHERE p.tenant_id = t.id
		          AND r.status IN ` + activeRunStatesSQL + `
		   )`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return lookupErr(err, "tenant", id)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var exists bool
	err = s.db.QueryRowContext(ctx, `SELECT true FROM tenants WHERE id = $1::uuid`, id).Scan(&exists)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: tenant %s", domain.ErrNotFound, id)
	case err != nil:
		return lookupErr(err, "tenant", id)
	default:
		return fmt.Errorf("%w: tenant %s masih punya run aktif", domain.ErrConflict, id)
	}
}

// --- connection ---

// CreateConnection menyimpan referensi koneksi (tanpa nilai kredensial).
func (s *Store) CreateConnection(ctx context.Context, c domain.Connection) (domain.Connection, error) {
	if c.Driver.RequiresDatabaseSecret() && c.SecretRef == "" && c.Password == "" {
		return domain.Connection{}, fmt.Errorf("%w: password atau secret_ref wajib diisi", domain.ErrInvalid)
	}
	endpoint, err := jsonParam(c.Endpoint)
	if err != nil {
		return domain.Connection{}, err
	}
	options, err := jsonParam(c.Options)
	if err != nil {
		return domain.Connection{}, err
	}

	const q = `
		INSERT INTO connections (id, tenant_id, name, kind, driver, endpoint, username, secret_ref,
		                         plaintext_password, options, created_at)
		VALUES (COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()), $2::uuid, $3, $4, $5,
		        COALESCE($6::jsonb, '{}'::jsonb), NULLIF($7,''), NULLIF($8,''),
		        NULLIF($9,''), COALESCE($10::jsonb, '{}'::jsonb), COALESCE($11::timestamptz, now()))
		RETURNING ` + connectionCols

	out, err := scanConnection(s.db.QueryRowContext(ctx, q,
		c.ID, c.TenantID, c.Name, string(c.Kind), string(c.Driver), endpoint, c.Username,
		c.SecretRef, c.Password, options, tsParam(c.CreatedAt)))
	if err != nil {
		return domain.Connection{}, classifyInsert(err, insertErrContext{
			uniqueMsg: fmt.Sprintf("koneksi %q sudah ada", c.Name),
			fkMsg:     fmt.Sprintf("tenant %s", c.TenantID),
			what:      "koneksi",
		})
	}
	return out, nil
}

// GetConnection mengambil koneksi berdasarkan id.
func (s *Store) GetConnection(ctx context.Context, id string) (domain.Connection, error) {
	q := `SELECT ` + connectionCols + ` FROM connections WHERE id = $1::uuid`
	c, err := scanConnection(s.db.QueryRowContext(ctx, q, id))
	if err != nil {
		return domain.Connection{}, lookupErr(err, "koneksi", id)
	}
	return c, nil
}

// ListConnections mengembalikan koneksi milik tenant, atau seluruhnya bila
// tenantID kosong.
func (s *Store) ListConnections(ctx context.Context, tenantID string) ([]domain.Connection, error) {
	q := `SELECT ` + connectionCols + ` FROM connections`
	args := []any{}
	if tenantID != "" {
		q += ` WHERE tenant_id = $1::uuid`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at, id`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Connection, 0)
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpdateConnection memperbarui definisi koneksi tanpa mengubah CreatedAt.
func (s *Store) UpdateConnection(ctx context.Context, c domain.Connection) (domain.Connection, error) {
	if c.Driver.RequiresDatabaseSecret() && c.SecretRef == "" && c.Password == "" {
		return domain.Connection{}, fmt.Errorf("%w: password atau secret_ref wajib diisi", domain.ErrInvalid)
	}
	endpoint, err := jsonParam(c.Endpoint)
	if err != nil {
		return domain.Connection{}, err
	}
	options, err := jsonParam(c.Options)
	if err != nil {
		return domain.Connection{}, err
	}
	const q = `
		UPDATE connections
		   SET tenant_id = $2::uuid, name = $3, kind = $4, driver = $5,
		       endpoint = COALESCE($6::jsonb, '{}'::jsonb), username = NULLIF($7,''),
		       secret_ref = NULLIF($8,''), plaintext_password = NULLIF($9,''),
		       options = COALESCE($10::jsonb, '{}'::jsonb)
		 WHERE id = $1::uuid
		RETURNING ` + connectionCols
	out, err := scanConnection(s.db.QueryRowContext(ctx, q,
		c.ID, c.TenantID, c.Name, string(c.Kind), string(c.Driver), endpoint,
		c.Username, c.SecretRef, c.Password, options))
	if err != nil {
		switch pgErrCode(err) {
		case sqlstateUnique:
			return domain.Connection{}, fmt.Errorf(
				"%w: koneksi %q sudah ada", domain.ErrConflict, c.Name)
		case sqlstateForeignKey:
			return domain.Connection{}, fmt.Errorf(
				"%w: tenant %s", domain.ErrNotFound, c.TenantID)
		case sqlstateCheck:
			return domain.Connection{}, fmt.Errorf(
				"%w: koneksi ditolak constraint %s",
				domain.ErrInvalid, pgErrConstraint(err))
		}
		return domain.Connection{}, lookupErr(err, "koneksi", c.ID)
	}
	return out, nil
}

// DeleteConnection menolak penghapusan selama koneksi masih dipakai pipeline.
func (s *Store) DeleteConnection(ctx context.Context, id string) error {
	const q = `
		DELETE FROM connections c
		 WHERE c.id = $1::uuid
		   AND NOT EXISTS (
		       SELECT 1 FROM pipelines p
		        WHERE p.source_connection_id = c.id
		           OR p.destination_connection_id = c.id
		   )`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return lookupErr(err, "koneksi", id)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	var exists bool
	err = s.db.QueryRowContext(ctx, `SELECT true FROM connections WHERE id = $1::uuid`, id).Scan(&exists)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: koneksi %s", domain.ErrNotFound, id)
	case err != nil:
		return lookupErr(err, "koneksi", id)
	default:
		return fmt.Errorf("%w: koneksi %s masih dipakai pipeline", domain.ErrConflict, id)
	}
}

// --- pipeline ---

// CreatePipeline menyimpan definisi pipeline.
func (s *Store) CreatePipeline(ctx context.Context, p domain.Pipeline) (domain.Pipeline, error) {
	if p.StateIDSuffix != nil && strings.Contains(*p.StateIDSuffix, ":") {
		return domain.Pipeline{}, fmt.Errorf(
			"%w: state_id_suffix tidak boleh mengandung ':'", domain.ErrInvalid)
	}
	timeout := p.DefaultTimeoutSeconds
	if timeout == 0 {
		timeout = 3600
	}
	maxAttempts := p.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	filterJSON, err := json.Marshal(p.SourceFilter)
	if err != nil {
		return domain.Pipeline{}, fmt.Errorf("source_filter pipeline: %w", err)
	}
	sourceFilter := string(filterJSON)
	transferJSON, err := json.Marshal(p.TransferOptions)
	if err != nil {
		return domain.Pipeline{}, fmt.Errorf("transfer_options pipeline: %w", err)
	}
	transferOptions := string(transferJSON)

	const q = `
		INSERT INTO pipelines (id, tenant_id, name, extractor, loader, source_connection_id,
		                       destination_connection_id, state_id_suffix, default_timeout_seconds,
		                       max_attempts, enabled, source_filter, transfer_options,
		                       created_at, updated_at)
		VALUES (COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()), $2::uuid, $3, $4, $5,
		        NULLIF($6,'')::uuid, NULLIF($7,'')::uuid, NULLIF($8,''), $9::integer, $10::integer,
		        $11::boolean, COALESCE($12::jsonb, '{}'::jsonb),
		        COALESCE($13::jsonb, '{}'::jsonb), COALESCE($14::timestamptz, now()), now())
		RETURNING ` + pipelineCols

	out, err := scanPipeline(s.db.QueryRowContext(ctx, q,
		p.ID, p.TenantID, p.Name, p.Extractor, p.Loader, p.SourceConnectionID,
		p.DestinationConnectionID, derefText(p.StateIDSuffix), timeout, maxAttempts,
		p.Enabled, sourceFilter, transferOptions, tsParam(p.CreatedAt)))
	if err != nil {
		return domain.Pipeline{}, classifyInsert(err, insertErrContext{
			uniqueMsg: fmt.Sprintf("pipeline %q sudah ada", p.Name),
			fkMsg:     fmt.Sprintf("tenant %s", p.TenantID),
			what:      "pipeline",
		})
	}
	return out, nil
}

// GetPipeline mengambil pipeline berdasarkan id.
func (s *Store) GetPipeline(ctx context.Context, id string) (domain.Pipeline, error) {
	q := `SELECT ` + pipelineCols + ` FROM pipelines WHERE id = $1::uuid`
	p, err := scanPipeline(s.db.QueryRowContext(ctx, q, id))
	if err != nil {
		return domain.Pipeline{}, lookupErr(err, "pipeline", id)
	}
	return p, nil
}

// ListPipelines mengembalikan pipeline milik tenant, atau seluruhnya bila
// tenantID kosong.
func (s *Store) ListPipelines(ctx context.Context, tenantID string) ([]domain.Pipeline, error) {
	q := `SELECT ` + pipelineCols + ` FROM pipelines`
	args := []any{}
	if tenantID != "" {
		q += ` WHERE tenant_id = $1::uuid`
		args = append(args, tenantID)
	}
	q += ` ORDER BY created_at, id`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Pipeline, 0)
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdatePipeline memperbarui definisi pipeline selama tidak ada run aktif.
func (s *Store) UpdatePipeline(ctx context.Context, p domain.Pipeline) (domain.Pipeline, error) {
	if p.StateIDSuffix != nil && strings.Contains(*p.StateIDSuffix, ":") {
		return domain.Pipeline{}, fmt.Errorf(
			"%w: state_id_suffix tidak boleh mengandung ':'", domain.ErrInvalid)
	}
	filterJSON, err := json.Marshal(p.SourceFilter)
	if err != nil {
		return domain.Pipeline{}, fmt.Errorf("source_filter pipeline: %w", err)
	}
	sourceFilter := string(filterJSON)
	transferJSON, err := json.Marshal(p.TransferOptions)
	if err != nil {
		return domain.Pipeline{}, fmt.Errorf("transfer_options pipeline: %w", err)
	}
	transferOptions := string(transferJSON)
	const q = `
		UPDATE pipelines p
		   SET tenant_id = $2::uuid, name = $3, extractor = $4, loader = $5,
		       source_connection_id = NULLIF($6,'')::uuid,
		       destination_connection_id = NULLIF($7,'')::uuid,
		       state_id_suffix = NULLIF($8,''), default_timeout_seconds = $9::integer,
		       max_attempts = $10::integer, enabled = $11::boolean,
		       source_filter = COALESCE($12::jsonb, '{}'::jsonb),
		       transfer_options = COALESCE($13::jsonb, '{}'::jsonb), updated_at = now()
		 WHERE p.id = $1::uuid
		   AND NOT EXISTS (
		       SELECT 1 FROM runs r
		        WHERE r.pipeline_id = p.id AND r.status IN ` + activeRunStatesSQL + `
		   )
		RETURNING ` + pipelineCols
	out, err := scanPipeline(s.db.QueryRowContext(ctx, q,
		p.ID, p.TenantID, p.Name, p.Extractor, p.Loader, p.SourceConnectionID,
		p.DestinationConnectionID, derefText(p.StateIDSuffix), p.DefaultTimeoutSeconds,
		p.MaxAttempts, p.Enabled, sourceFilter, transferOptions))
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		existsErr := s.db.QueryRowContext(ctx,
			`SELECT true FROM pipelines WHERE id = $1::uuid`, p.ID).Scan(&exists)
		if errors.Is(existsErr, sql.ErrNoRows) {
			return domain.Pipeline{}, fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, p.ID)
		}
		if existsErr != nil {
			return domain.Pipeline{}, lookupErr(existsErr, "pipeline", p.ID)
		}
		return domain.Pipeline{}, fmt.Errorf("%w: pipeline %s masih punya run aktif",
			domain.ErrConflict, p.ID)
	}
	if err != nil {
		return domain.Pipeline{}, classifyInsert(err, insertErrContext{
			uniqueMsg: fmt.Sprintf("pipeline %q sudah ada", p.Name),
			fkMsg:     fmt.Sprintf("tenant %s", p.TenantID),
			what:      "pipeline",
		})
	}
	return out, nil
}

// DeletePipeline menghapus pipeline yang tidak punya run aktif.
func (s *Store) DeletePipeline(ctx context.Context, id string) error {
	const q = `
		DELETE FROM pipelines
		 WHERE id = $1::uuid
		   AND NOT EXISTS (SELECT 1 FROM runs
		                    WHERE pipeline_id = $1::uuid AND status IN ` + activeRunStatesSQL + `)`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return lookupErr(err, "pipeline", id)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	// Tidak ada baris yang terhapus: bedakan "tidak ada" dari "masih aktif".
	var exists bool
	err = s.db.QueryRowContext(ctx, `SELECT true FROM pipelines WHERE id = $1::uuid`, id).Scan(&exists)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: pipeline %s", domain.ErrNotFound, id)
	case err != nil:
		return lookupErr(err, "pipeline", id)
	default:
		return fmt.Errorf("%w: pipeline %s masih punya run aktif", domain.ErrConflict, id)
	}
}

// --- run ---

// CreateRun menyimpan run baru berstatus queued.
//
// Dua pengaman datang dari index unik di skema: satu run aktif per pipeline
// (menggantikan pengaman advisory Meltano), dan satu idempotency key.
func (s *Store) CreateRun(ctx context.Context, run domain.Run, idempotencyKey string) (domain.Run, error) {
	if idempotencyKey != "" {
		existing, err := s.runByIdempotencyKey(ctx, idempotencyKey)
		switch {
		case err == nil:
			return existing, nil
		case !errors.Is(err, sql.ErrNoRows):
			return domain.Run{}, err
		}
	}

	pipeline, err := s.GetPipeline(ctx, run.PipelineID)
	if err != nil {
		return domain.Run{}, err
	}
	if !pipeline.Enabled {
		return domain.Run{}, fmt.Errorf("%w: pipeline %s tidak aktif", domain.ErrInvalid, pipeline.ID)
	}

	status := run.Status
	if status == "" {
		status = domain.StatusQueued
	}
	attempt := run.Attempt
	if attempt == 0 {
		attempt = 1
	}
	trigger := run.Trigger
	if trigger == "" {
		trigger = "manual"
	}
	timeout := run.TimeoutSeconds
	if timeout == 0 {
		timeout = pipeline.DefaultTimeoutSeconds
	}

	const q = `
		INSERT INTO runs (id, pipeline_id, status, attempt, auto_attempts, retried_from, trigger,
		                  reason, timeout_seconds, full_refresh, state_id_suffix, idempotency_key,
		                  created_at)
		VALUES (COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()), $2::uuid, $3, $4::integer,
		        $5::integer, NULLIF($6,'')::uuid, $7, NULLIF($8,''), $9::integer, $10::boolean,
		        NULLIF($11,''), NULLIF($12,''), now())
		RETURNING ` + runCols

	created, err := scanRun(s.db.QueryRowContext(ctx, q,
		run.ID, run.PipelineID, string(status), attempt, run.AutoAttempts, derefText(run.RetriedFrom),
		trigger, run.Reason, timeout, run.FullRefresh, derefText(run.StateIDSuffix), idempotencyKey))
	if err == nil {
		return created, nil
	}
	if pgErrCode(err) != sqlstateUnique {
		return domain.Run{}, err
	}

	// Index unik mana yang kena? Kalau idempotency key, run yang sudah ada yang
	// menang (balapan dua request identik). Kalau slot run aktif, ini konflik
	// yang memang ingin dilaporkan ke pemanggil.
	if pgErrConstraint(err) == "ux_runs_idempotency" && idempotencyKey != "" {
		if existing, lookupErr := s.runByIdempotencyKey(ctx, idempotencyKey); lookupErr == nil {
			return existing, nil
		}
	}
	return domain.Run{}, s.activeRunConflict(ctx, run.PipelineID)
}

// GetRun mengambil run berdasarkan id.
func (s *Store) GetRun(ctx context.Context, id string) (domain.Run, error) {
	q := `SELECT ` + runCols + ` FROM runs WHERE id = $1::uuid`
	r, err := scanRun(s.db.QueryRowContext(ctx, q, id))
	if err != nil {
		return domain.Run{}, lookupErr(err, "run", id)
	}
	return r, nil
}

// ListRuns mengembalikan run terbaru lebih dulu.
func (s *Store) ListRuns(ctx context.Context, filter store.RunFilter) ([]domain.Run, error) {
	q := `SELECT ` + runCols + ` FROM runs`
	where := make([]string, 0, 2)
	args := make([]any, 0, 4)

	if filter.PipelineID != "" {
		args = append(args, filter.PipelineID)
		where = append(where, fmt.Sprintf("pipeline_id = $%d::uuid", len(args)))
	}
	if len(filter.Statuses) > 0 {
		placeholders := make([]string, 0, len(filter.Statuses))
		for _, st := range filter.Statuses {
			args = append(args, string(st))
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		where = append(where, "status IN ("+strings.Join(placeholders, ",")+")")
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY created_at DESC, id DESC"
	if filter.Limit > 0 {
		args = append(args, filter.Limit)
		q += fmt.Sprintf(" LIMIT $%d::integer", len(args))
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Run, 0)
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClaimRun mengambil satu run queued secara atomik dan menandainya running.
//
// FOR UPDATE SKIP LOCKED membuat dua worker tidak pernah mengambil run yang
// sama, dan worker yang lebih lambat tidak menunggu worker lain.
func (s *Store) ClaimRun(ctx context.Context, workerID string, lease time.Duration) (*domain.Run, error) {
	const q = `
		UPDATE runs
		   SET status = 'running',
		       claimed_by = $1,
		       lease_expires_at = now() + ($2::double precision * interval '1 second'),
		       started_at = COALESCE(started_at, now())
		 WHERE id = (SELECT id FROM runs
		              WHERE status = 'queued'
		              ORDER BY created_at, id
		              FOR UPDATE SKIP LOCKED
		              LIMIT 1)
		RETURNING ` + runCols

	run, err := scanRun(s.db.QueryRowContext(ctx, q, workerID, lease.Seconds()))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// CompleteRun menulis hasil terminal (atau mengantrekan ulang untuk retry).
func (s *Store) CompleteRun(ctx context.Context, id string, c store.Completion) (domain.Run, error) {
	const q = `
		UPDATE runs
		   SET status = $2,
		       exit_code = $3::integer,
		       error = NULLIF($4,''),
		       log_tail = NULLIF($5,''),
		       meltano_state_id = COALESCE(NULLIF($6,''), meltano_state_id),
		       meltano_run_id = COALESCE(NULLIF($7,'')::uuid, meltano_run_id),
		       auto_attempts = GREATEST(auto_attempts, $8::integer),
		       claimed_by = CASE WHEN $2 = 'queued' THEN NULL ELSE claimed_by END,
		       lease_expires_at = CASE WHEN $2 = 'queued' THEN NULL ELSE lease_expires_at END,
		       ended_at = CASE WHEN $2 IN ` + terminalStatesSQL + ` THEN now() ELSE NULL END
		 WHERE id = $1::uuid
		RETURNING ` + runCols

	out, err := scanRun(s.db.QueryRowContext(ctx, q,
		id, string(c.Status), intOrNil(c.ExitCode), c.Error, c.LogTail,
		c.MeltanoStateID, c.MeltanoRunID, c.AutoAttempts))
	if err != nil {
		return domain.Run{}, lookupErr(err, "run", id)
	}
	return out, nil
}

// HeartbeatRun memperpanjang lease run yang sedang berjalan.
func (s *Store) HeartbeatRun(ctx context.Context, id, workerID string, lease time.Duration) error {
	const q = `
		UPDATE runs
		   SET lease_expires_at = now() + ($3::double precision * interval '1 second')
		 WHERE id = $1::uuid AND claimed_by = $2 AND status IN ('running','cancelling')`
	res, err := s.db.ExecContext(ctx, q, id, workerID, lease.Seconds())
	if err != nil {
		return lookupErr(err, "run", id)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	var status string
	var claimedBy sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT status, claimed_by FROM runs WHERE id = $1::uuid`, id).Scan(&status, &claimedBy)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%w: run %s", domain.ErrNotFound, id)
	case err != nil:
		return lookupErr(err, "run", id)
	case domain.RunStatus(status).IsTerminal():
		return fmt.Errorf("%w: run %s sudah selesai", domain.ErrConflict, id)
	case !claimedBy.Valid || claimedBy.String != workerID:
		return fmt.Errorf("%w: run %s bukan milik worker %s", domain.ErrConflict, id, workerID)
	default:
		return fmt.Errorf("%w: run %s belum diklaim", domain.ErrConflict, id)
	}
}

// RequestCancel menandai run untuk dibatalkan. Idempoten.
func (s *Store) RequestCancel(ctx context.Context, id string) (domain.Run, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Run{}, err
	}
	defer func() { _ = tx.Rollback() }()

	locked := `SELECT ` + runCols + ` FROM runs WHERE id = $1::uuid FOR UPDATE`
	current, err := scanRun(tx.QueryRowContext(ctx, locked, id))
	if err != nil {
		return domain.Run{}, lookupErr(err, "run", id)
	}
	if current.Status.IsTerminal() {
		return current, nil // idempoten
	}

	if current.Status == domain.StatusQueued {
		// Belum ada worker yang memegang: bisa langsung ditutup.
		_, err = tx.ExecContext(ctx,
			`UPDATE runs SET status = 'cancelled', ended_at = now() WHERE id = $1::uuid`, id)
	} else {
		// Worker yang sedang berjalan akan melihat status ini.
		_, err = tx.ExecContext(ctx,
			`UPDATE runs SET status = 'cancelling' WHERE id = $1::uuid`, id)
	}
	if err != nil {
		return domain.Run{}, lookupErr(err, "run", id)
	}

	updated, err := scanRun(tx.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE id = $1::uuid`, id))
	if err != nil {
		return domain.Run{}, lookupErr(err, "run", id)
	}
	if err := tx.Commit(); err != nil {
		return domain.Run{}, err
	}
	return updated, nil
}

// ReapExpired menandai run yang lease-nya habis sebagai failed.
func (s *Store) ReapExpired(ctx context.Context, now time.Time) (int, error) {
	const q = `
		UPDATE runs
		   SET status = 'failed',
		       error = $2,
		       ended_at = $1::timestamptz,
		       lease_expires_at = NULL
		 WHERE status IN ('running','cancelling')
		   AND lease_expires_at IS NOT NULL
		   AND lease_expires_at <= $1::timestamptz`
	res, err := s.db.ExecContext(ctx, q, now.UTC().Format(time.RFC3339Nano), leaseExpiredReason)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// --- event ---

// AppendEvent menambahkan satu jejak run.
func (s *Store) AppendEvent(ctx context.Context, ev domain.RunEvent) error {
	payload, err := jsonParam(ev.Payload)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO run_events (run_id, at, kind, payload)
		VALUES ($1::uuid, COALESCE($2::timestamptz, now()), $3, COALESCE($4::jsonb, '{}'::jsonb))`
	if _, err := s.db.ExecContext(ctx, q, ev.RunID, tsParam(ev.At), ev.Kind, payload); err != nil {
		if pgErrCode(err) == sqlstateForeignKey {
			return fmt.Errorf("%w: run %s", domain.ErrNotFound, ev.RunID)
		}
		return lookupErr(err, "run", ev.RunID)
	}
	return nil
}

// ListEvents mengembalikan event run, terlama lebih dulu.
//
// limit <= 0 berarti tanpa batas. Kalau dibatasi, yang diambil adalah limit
// event terakhir supaya yang tampil tetap peristiwa terbaru.
func (s *Store) ListEvents(ctx context.Context, runID string, limit int) ([]domain.RunEvent, error) {
	const inner = `SELECT id, at, kind, payload FROM run_events WHERE run_id = $1::uuid
	               ORDER BY at DESC, id DESC LIMIT $2::integer`
	const q = `SELECT id, at, kind, payload FROM (` + inner + `) AS tail ORDER BY at, id`

	var limitArg any
	if limit > 0 {
		limitArg = limit
	}
	rows, err := s.db.QueryContext(ctx, q, runID, limitArg)
	if err != nil {
		return nil, lookupErr(err, "run", runID)
	}
	defer rows.Close()

	out := make([]domain.RunEvent, 0)
	for rows.Next() {
		var (
			ev      domain.RunEvent
			rawJSON []byte
		)
		if err := rows.Scan(&ev.ID, &ev.At, &ev.Kind, &rawJSON); err != nil {
			return nil, err
		}
		ev.RunID = runID
		if err := json.Unmarshal(rawJSON, &ev.Payload); err != nil {
			return nil, fmt.Errorf("payload event %d bukan JSON: %w", ev.ID, err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// --- helper ---

const connectionCols = `id::text, tenant_id::text, name, kind, driver, endpoint,
	COALESCE(username,''), COALESCE(secret_ref,''), COALESCE(plaintext_password,''), options, created_at`

const pipelineCols = `id::text, tenant_id::text, name, extractor, loader,
	COALESCE(source_connection_id::text,''), COALESCE(destination_connection_id::text,''),
	state_id_suffix, default_timeout_seconds, max_attempts, enabled, source_filter,
	transfer_options, created_at, updated_at`

const runCols = `id::text, pipeline_id::text, status, attempt, auto_attempts, retried_from::text,
	trigger, reason, meltano_state_id, meltano_run_id::text, exit_code, error, log_tail,
	claimed_by, lease_expires_at, created_at, started_at, ended_at,
	timeout_seconds, full_refresh, state_id_suffix`

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (domain.User, error) {
	var (
		user domain.User
		role string
	)
	err := row.Scan(
		&user.ID, &user.Username, &user.DisplayName, &role, &user.PasswordHash,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		return domain.User{}, err
	}
	user.Role = domain.UserRole(role)
	return user, nil
}

func scanTenant(row scanner) (domain.Tenant, error) {
	var t domain.Tenant
	err := row.Scan(&t.ID, &t.Name, &t.DisplayName, &t.EnvironmentName, &t.ProjectDir, &t.CreatedAt)
	if err != nil {
		return domain.Tenant{}, err
	}
	return t, nil
}

func scanConnection(row scanner) (domain.Connection, error) {
	var (
		c        domain.Connection
		kind     string
		driver   string
		endpoint []byte
		options  []byte
	)
	err := row.Scan(&c.ID, &c.TenantID, &c.Name, &kind, &driver, &endpoint, &c.Username,
		&c.SecretRef, &c.Password, &options, &c.CreatedAt)
	if err != nil {
		return domain.Connection{}, err
	}
	c.Kind = domain.ConnectionKind(kind)
	c.Driver = domain.Driver(driver)
	if c.Endpoint, err = jsonMap(endpoint); err != nil {
		return domain.Connection{}, fmt.Errorf("endpoint koneksi %s: %w", c.ID, err)
	}
	if c.Options, err = jsonMap(options); err != nil {
		return domain.Connection{}, fmt.Errorf("options koneksi %s: %w", c.ID, err)
	}
	return c, nil
}

func scanPipeline(row scanner) (domain.Pipeline, error) {
	var (
		p               domain.Pipeline
		suffix          sql.NullString
		sourceFilter    []byte
		transferOptions []byte
	)
	err := row.Scan(&p.ID, &p.TenantID, &p.Name, &p.Extractor, &p.Loader, &p.SourceConnectionID,
		&p.DestinationConnectionID, &suffix, &p.DefaultTimeoutSeconds, &p.MaxAttempts, &p.Enabled,
		&sourceFilter, &transferOptions, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.Pipeline{}, err
	}
	if len(sourceFilter) > 0 {
		if err := json.Unmarshal(sourceFilter, &p.SourceFilter); err != nil {
			return domain.Pipeline{}, fmt.Errorf("source_filter pipeline %s: %w", p.ID, err)
		}
	}
	if len(transferOptions) > 0 {
		if err := json.Unmarshal(transferOptions, &p.TransferOptions); err != nil {
			return domain.Pipeline{}, fmt.Errorf("transfer_options pipeline %s: %w", p.ID, err)
		}
	}
	if suffix.Valid {
		v := suffix.String
		p.StateIDSuffix = &v
	}
	return p, nil
}

func scanRun(row scanner) (domain.Run, error) {
	var (
		run          domain.Run
		status       string
		retriedFrom  sql.NullString
		reason       sql.NullString
		stateID      sql.NullString
		meltanoRunID sql.NullString
		exitCode     sql.NullInt64
		errText      sql.NullString
		logTail      sql.NullString
		claimedBy    sql.NullString
		leaseExpires sql.NullTime
		startedAt    sql.NullTime
		endedAt      sql.NullTime
		stateSuffix  sql.NullString
	)
	err := row.Scan(&run.ID, &run.PipelineID, &status, &run.Attempt, &run.AutoAttempts, &retriedFrom,
		&run.Trigger, &reason, &stateID, &meltanoRunID, &exitCode, &errText, &logTail,
		&claimedBy, &leaseExpires, &run.CreatedAt, &startedAt, &endedAt,
		&run.TimeoutSeconds, &run.FullRefresh, &stateSuffix)
	if err != nil {
		return domain.Run{}, err
	}

	run.Status = domain.RunStatus(status)
	if retriedFrom.Valid {
		v := retriedFrom.String
		run.RetriedFrom = &v
	}
	if reason.Valid {
		run.Reason = reason.String
	}
	if stateID.Valid {
		run.MeltanoStateID = stateID.String
	}
	if meltanoRunID.Valid {
		run.MeltanoRunID = meltanoRunID.String
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		run.ExitCode = &v
	}
	if errText.Valid {
		run.Error = errText.String
	}
	if logTail.Valid {
		run.LogTail = logTail.String
	}
	if claimedBy.Valid {
		v := claimedBy.String
		run.ClaimedBy = &v
	}
	if leaseExpires.Valid {
		v := leaseExpires.Time
		run.LeaseExpiresAt = &v
	}
	if startedAt.Valid {
		v := startedAt.Time
		run.StartedAt = &v
	}
	if endedAt.Valid {
		v := endedAt.Time
		run.EndedAt = &v
	}
	if stateSuffix.Valid {
		v := stateSuffix.String
		run.StateIDSuffix = &v
	}
	return run, nil
}

// runByIdempotencyKey mengambil run yang sudah tercatat untuk sebuah key.
// Mengembalikan sql.ErrNoRows bila belum ada.
func (s *Store) runByIdempotencyKey(ctx context.Context, key string) (domain.Run, error) {
	q := `SELECT ` + runCols + ` FROM runs WHERE idempotency_key = $1`
	return scanRun(s.db.QueryRowContext(ctx, q, key))
}

// activeRunConflict menyusun pesan konflik yang menyebut run aktif yang ada,
// supaya pemanggil tahu run mana yang harus ditunggu atau dibatalkan.
func (s *Store) activeRunConflict(ctx context.Context, pipelineID string) error {
	const q = `SELECT id::text, status FROM runs
	            WHERE pipeline_id = $1::uuid AND status IN ` + activeRunStatesSQL + `
	            ORDER BY created_at LIMIT 1`
	var (
		id     string
		status string
	)
	if err := s.db.QueryRowContext(ctx, q, pipelineID).Scan(&id, &status); err != nil {
		return fmt.Errorf("%w: pipeline %s sudah punya run aktif", domain.ErrConflict, pipelineID)
	}
	return fmt.Errorf("%w: pipeline %s sudah punya run aktif %s (%s)",
		domain.ErrConflict, pipelineID, id, status)
}

type insertErrContext struct {
	uniqueMsg string
	fkMsg     string
	what      string
}

// classifyInsert memetakan pelanggaran constraint saat INSERT ke error domain.
func classifyInsert(err error, ctx insertErrContext) error {
	switch pgErrCode(err) {
	case sqlstateUnique:
		return fmt.Errorf("%w: %s", domain.ErrConflict, ctx.uniqueMsg)
	case sqlstateForeignKey:
		return fmt.Errorf("%w: %s tidak ditemukan", domain.ErrNotFound, ctx.fkMsg)
	case sqlstateCheck:
		return fmt.Errorf("%w: %s ditolak constraint %s", domain.ErrInvalid, ctx.what, pgErrConstraint(err))
	default:
		return err
	}
}

// lookupErr memetakan kegagalan baca satu baris ke error domain.
func lookupErr(err error, kind, id string) error {
	switch {
	case errors.Is(err, sql.ErrNoRows), pgErrCode(err) == sqlstateInvalidText:
		return fmt.Errorf("%w: %s %s", domain.ErrNotFound, kind, id)
	case pgErrCode(err) == sqlstateCheck:
		return fmt.Errorf("%w: %s %s ditolak constraint %s", domain.ErrInvalid, kind, id, pgErrConstraint(err))
	default:
		return err
	}
}

func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func pgErrConstraint(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

// tsParam mengubah waktu menjadi string (dengan cast ::timestamptz di SQL),
// atau nil agar kolomnya NULL.
func tsParam(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func intOrNil(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func derefText(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}

func jsonParam(m map[string]any) (any, error) {
	if len(m) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode json: %w", err)
	}
	return string(raw), nil
}

func jsonMap(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
