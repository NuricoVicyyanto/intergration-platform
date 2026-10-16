// Package obs menyediakan logging terstruktur.
package obs

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger mengembalikan logger JSON ke stdout.
//
// Format JSON dipilih agar sejalan dengan `meltano --log-format=json`
// (lihat doc 15 bagian 15.7.2) sehingga log control plane dan log Meltano
// dapat dikonsumsi oleh pipeline yang sama.
func NewLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(h)
}
