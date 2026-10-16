package meltano

import (
	"strings"

	"github.com/example/controlplane/internal/domain"
)

// Exit code yang dikenal dari Meltano (lihat doc 13 bagian 13.5.4):
//
//	0   sukses            (job/job.py:331-332)
//	1   gagal             (cli/utils.py:54) atau timeout (cli/run.py:249)
//	3   versi tidak cocok (cli/cli.py:172-181)
//	143 SIGTERM           (job/job.py:43)
const (
	ExitSuccess         = 0
	ExitFailure         = 1
	ExitVersionMismatch = 3
	ExitSIGTERM         = 143 // 128 + 15
)

// Marker yang dicari pada log saat menaikkan exit code 1 menjadi timed_out.
// Sumbernya pesan logger.error("Run timed out", ...) di src/meltano/cli/run.py:243.
const timeoutMarker = "Run timed out"

// Classification adalah hasil penerjemahan exit code menjadi keputusan control plane.
type Classification struct {
	Status domain.RunStatus
	// Retryable menandai apakah kegagalan layak dicoba ulang otomatis.
	// Hanya exit code 1 yang retryable; 3 dan kode tak dikenal tidak.
	Retryable bool
	// Reason adalah penjelasan singkat untuk log dan kolom error.
	Reason string
}

// Classify menerjemahkan exit code dan potongan log menjadi keputusan.
//
// Aturan retry (doc 14 bagian 14.2.3):
//   - 0   -> succeeded
//   - 1   -> failed (retryable), atau timed_out bila log memuat penanda timeout
//   - 3   -> failed (TIDAK retryable); versi Meltano tidak cocok dengan project
//   - 143 -> cancelled
//   - lain -> failed (TIDAK retryable)
//
// cancelled hanya dipakai bila kita tahu SIGTERM kita yang dikirim. Karena fungsi
// ini tidak memegang konteks itu, pemanggil boleh menimpanya; lihat Adapter.Run.
func Classify(exitCode int, logTail string) Classification {
	switch exitCode {
	case ExitSuccess:
		return Classification{Status: domain.StatusSucceeded}

	case ExitSIGTERM:
		return Classification{
			Status: domain.StatusCancelled,
			Reason: "proses dihentikan dengan SIGTERM (exit code 143)",
		}

	case ExitVersionMismatch:
		return Classification{
			Status:    domain.StatusFailed,
			Retryable: false,
			Reason:    "versi Meltano tidak kompatibel dengan project (exit code 3); jangan retry",
		}

	case ExitFailure:
		if strings.Contains(logTail, timeoutMarker) {
			return Classification{
				Status:    domain.StatusTimedOut,
				Retryable: true,
				Reason:    "run melewati batas waktu (--timeout)",
			}
		}
		return Classification{
			Status:    domain.StatusFailed,
			Retryable: true,
			Reason:    "pipeline gagal (exit code 1)",
		}

	default:
		return Classification{
			Status:    domain.StatusFailed,
			Retryable: false,
			Reason:    "exit code tak dikenal; periksa log dan jangan retry otomatis",
		}
	}
}
