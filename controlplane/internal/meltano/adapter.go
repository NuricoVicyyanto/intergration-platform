// Package meltano adalah satu-satunya titik integrasi ke CLI Meltano.
//
// Semua pengetahuan tentang argv, environment, exit code, dan state_id
// terkumpul di paket ini (lihat meltano-go-reference/13 bagian 13.3).
// Kalau Meltano berubah, hanya paket ini yang perlu disesuaikan.
package meltano

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/example/controlplane/internal/domain"
)

// DefaultGracePeriod adalah waktu yang diberikan kepada Meltano untuk
// menyelesaikan graceful shutdown setelah SIGTERM sebelum di-SIGKILL.
// Meltano memasang handler SIGTERM sendiri dan meneruskannya ke tap/target
// (src/meltano/core/job/job.py:448-496).
const DefaultGracePeriod = 30 * time.Second

// Adapter menjalankan proses Meltano.
type Adapter struct {
	// Bin adalah path executable Meltano. Kosong berarti "meltano" dari PATH.
	Bin string
	// Logger boleh nil.
	Logger *slog.Logger
}

// RunRequest adalah satu permintaan eksekusi.
type RunRequest struct {
	// ProjectDir adalah cwd proses dan harus berisi meltano.yml.
	ProjectDir string
	// Spec berisi parameter yang diterjemahkan menjadi argv.
	Spec RunSpec
	// Trigger dikirim sebagai MELTANO_JOB_TRIGGER sehingga muncul di kolom
	// runs.trigger dan pada output `meltano logs list --format json`.
	Trigger string
	// SystemDBURI menjadi MELTANO_DATABASE_URI. Kosong berarti memakai default
	// project (SQLite) - tidak disarankan untuk multi-worker.
	SystemDBURI string
	// ConnectorEnv berisi kredensial yang sudah di-resolve worker. Nilai di sini
	// tidak boleh pernah ditulis ke database atau log.
	ConnectorEnv map[string]string
	// ExtraEnv adalah tambahan bebas, misalnya setting khusus tenant.
	ExtraEnv map[string]string
	// LogTailLines adalah jumlah baris terakhir yang disimpan. Nol berarti 100.
	LogTailLines int
	// GracePeriod setelah SIGTERM sebelum paksa berhenti. Nol berarti
	// DefaultGracePeriod.
	GracePeriod time.Duration
}

// RunResult adalah hasil terminal satu eksekusi.
type RunResult struct {
	ExitCode    int
	Status      domain.RunStatus
	Retryable   bool
	Reason      string
	StateID     string
	StartedAt   time.Time
	EndedAt     time.Time
	LogTail     string
	CommandLine []string

	// TimedOut true bila context habis waktunya.
	TimedOut bool
	// Cancelled true bila context dibatalkan (bukan timeout).
	Cancelled bool
}

// Duration mengembalikan durasi eksekusi.
func (r RunResult) Duration() time.Duration { return r.EndedAt.Sub(r.StartedAt) }

// Run menjalankan satu `meltano run` sampai selesai.
//
// Fungsi ini selalu mengembalikan RunResult yang terisi saat proses berhasil
// dijalankan - termasuk untuk exit code non-nol. Error hanya dikembalikan bila
// proses tidak bisa dijalankan sama sekali (mis. executable tidak ditemukan)
// atau parameter tidak valid.
func (a *Adapter) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if strings.TrimSpace(req.ProjectDir) == "" {
		return RunResult{}, fmt.Errorf("%w: ProjectDir wajib diisi", domain.ErrInvalid)
	}
	if req.Spec.Extractor == "" || req.Spec.Loader == "" {
		return RunResult{}, fmt.Errorf("%w: Extractor dan Loader wajib diisi", domain.ErrInvalid)
	}

	stateID, err := StateID(
		req.Spec.Environment, req.Spec.Extractor, req.Spec.Loader, req.Spec.StateIDSuffix)
	if err != nil {
		return RunResult{}, fmt.Errorf("%w: %v", domain.ErrInvalid, err)
	}

	bin := a.Bin
	if bin == "" {
		bin = "meltano"
	}
	args := BuildArgs(req.Spec)

	tailLines := req.LogTailLines
	if tailLines <= 0 {
		tailLines = 100
	}
	grace := req.GracePeriod
	if grace <= 0 {
		grace = DefaultGracePeriod
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = req.ProjectDir
	cmd.Env = buildEnv(req)

	tail := newTailBuffer(tailLines)
	cmd.Stdout = tail
	cmd.Stderr = tail

	// Kirim SIGTERM lebih dulu supaya Meltano sempat melakukan graceful stop,
	// baru paksa berhenti setelah grace period.
	cmd.Cancel = func() error { return terminate(cmd.Process) }
	cmd.WaitDelay = grace

	res := RunResult{
		StateID:     stateID,
		CommandLine: append([]string{bin}, args...),
		StartedAt:   time.Now().UTC(),
	}
	if a.Logger != nil {
		a.Logger.Info("menjalankan meltano",
			slog.String("argv", strings.Join(res.CommandLine, " ")),
			slog.String("project_dir", req.ProjectDir),
			slog.String("state_id", stateID))
	}

	runErr := cmd.Run()
	res.EndedAt = time.Now().UTC()
	res.LogTail = tail.String()

	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}

	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.TimedOut = true
	case ctx.Err() == context.Canceled:
		res.Cancelled = true
	}

	// Kegagalan menjalankan proses (bukan kegagalan pipeline).
	if runErr != nil && cmd.ProcessState == nil {
		return res, fmt.Errorf("gagal menjalankan %s: %w", bin, runErr)
	}

	cls := Classify(res.ExitCode, res.LogTail)
	res.Status = cls.Status
	res.Retryable = cls.Retryable
	res.Reason = cls.Reason

	// Timeout/cancel yang kita picu sendiri lebih akurat daripada pembacaan log.
	if res.TimedOut {
		res.Status = domain.StatusTimedOut
		res.Retryable = true
		res.Reason = "run dihentikan karena melewati batas waktu"
	}
	if res.Cancelled {
		res.Status = domain.StatusCancelled
		res.Retryable = false
		res.Reason = "run dibatalkan atas permintaan"
	}

	if a.Logger != nil {
		a.Logger.Info("meltano selesai",
			slog.Int("exit_code", res.ExitCode),
			slog.String("status", string(res.Status)),
			slog.Duration("duration", res.Duration()),
			slog.String("state_id", stateID))
	}
	return res, nil
}

// buildEnv menyusun environment proses Meltano.
//
// Env var wajib beserta alasannya ada di meltano-go-reference/15 bagian 15.7.2.
func buildEnv(req RunRequest) []string {
	trigger := req.Trigger
	if trigger == "" {
		trigger = req.Spec.RunID
	}

	env := os.Environ()
	set := func(k, v string) {
		if v == "" {
			return
		}
		env = append(env, k+"="+v)
	}

	// Menghindari UnicodeEncodeError pada konsol Windows (lihat doc 15 bagian 15.3).
	env = append(env, "PYTHONUTF8=1")
	// Korelasi run: masuk ke kolom runs.trigger.
	set("MELTANO_JOB_TRIGGER", trigger)
	// Job tidak boleh memutasi file project.
	env = append(env, "MELTANO_PROJECT_READONLY=true")
	// Jangan pernah install plugin di jalur request.
	env = append(env, "MELTANO_AUTO_INSTALL=false")
	env = append(env, "MELTANO_SEND_ANONYMOUS_USAGE_STATS=false")

	// CATATAN: MELTANO_FF_STRICT_ENV_VAR_MODE sengaja TIDAK diset di sini.
	//
	// Mode itu membuat env var yang hilang menjadi error fatal, yang terdengar
	// aman, tetapi kenyataannya mematahkan plugin nyata: definisi plugin dapat
	// memuat nilai default yang mereferensikan env var milik paketnya sendiri.
	// Contoh terverifikasi pada tap-gitlab 4.4.1: "Environment variable
	// 'TITLEPREFIX' referenced but not set", yang membuat setiap run gagal
	// sebelum tap sempat jalan.
	//
	// Kalau tetap ingin mode ketat, aktifkan per tenant lewat req.ExtraEnv.

	// System database bersama supaya aman untuk banyak worker.
	set("MELTANO_DATABASE_URI", req.SystemDBURI)

	for k, v := range req.ExtraEnv {
		set(k, v)
	}
	for k, v := range req.ConnectorEnv {
		set(k, v)
	}
	return env
}

// tailBuffer menyimpan sejumlah baris terakhir output proses tanpa tumbuh tanpa batas.
type tailBuffer struct {
	mu    sync.Mutex
	max   int
	lines []string
	rest  []byte
}

func newTailBuffer(maxLines int) *tailBuffer {
	return &tailBuffer{max: maxLines, lines: make([]string, 0, maxLines)}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	buf := append(t.rest, p...)
	t.rest = nil
	for {
		i := indexByte(buf, '\n')
		if i < 0 {
			t.rest = append([]byte(nil), buf...)
			break
		}
		t.push(strings.TrimRight(string(buf[:i]), "\r"))
		buf = buf[i+1:]
	}
	return len(p), nil
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func (t *tailBuffer) push(line string) {
	if t.max <= 0 {
		return
	}
	if len(t.lines) == t.max {
		copy(t.lines, t.lines[1:])
		t.lines[len(t.lines)-1] = line
		return
	}
	t.lines = append(t.lines, line)
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.lines)+1)
	out = append(out, t.lines...)
	if len(t.rest) > 0 {
		out = append(out, string(t.rest))
	}
	return strings.Join(out, "\n")
}
