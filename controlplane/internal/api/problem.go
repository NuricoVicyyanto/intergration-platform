package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/example/controlplane/internal/domain"
)

// Problem mengikuti RFC 7807 (application/problem+json), sesuai
// meltano-go-reference/14 bagian 14.1.6.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`

	// Field tambahan yang spesifik untuk konflik run aktif.
	ActiveRunID string `json:"active_run_id,omitempty"`
}

// ErrorTypeBase adalah basis URL untuk tipe error.
const ErrorTypeBase = "https://errors.example.com/"

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Header sudah terkirim; tidak ada yang bisa dilakukan selain mencatat.
		slog.Default().Error("gagal menulis respons JSON", slog.Any("error", err))
	}
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, slug, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{
		Type:     ErrorTypeBase + slug,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: r.URL.Path,
	})
}

// writeError memetakan error domain ke status HTTP yang tepat.
//
// domain.ErrConflict sengaja dipetakan ke 409, bukan 429: konflik run aktif
// adalah kesalahan klien yang harus diperbaiki, bukan kondisi untuk backoff
// (lihat doc 14 bagian 14.10 nomor 4).
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not-found", "Resource not found", err.Error())
	case errors.Is(err, domain.ErrConflict):
		writeProblem(w, r, http.StatusConflict, "conflict", "Conflict", err.Error())
	case errors.Is(err, domain.ErrInvalid):
		writeProblem(w, r, http.StatusUnprocessableEntity,
			"unprocessable", "Request cannot be processed", err.Error())
	default:
		slog.Default().Error("kesalahan internal", slog.Any("error", err), slog.String("path", r.URL.Path))
		writeProblem(w, r, http.StatusInternalServerError,
			"internal", "Internal error", "terjadi kesalahan pada server")
	}
}

// decodeJSON membaca body JSON dengan batas ukuran dan menolak field tak dikenal.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeProblem(w, r, http.StatusBadRequest, "bad-request", "Malformed request body", err.Error())
		return false
	}
	return true
}
