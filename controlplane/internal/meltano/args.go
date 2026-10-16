package meltano

import "strconv"

// LogFormatJSON adalah nilai --log-format yang dipakai agar log Meltano
// terstruktur dan bisa dikorelasi lewat field run_id.
const LogFormatJSON = "json"

// RunSpec adalah parameter satu eksekusi pipeline.
type RunSpec struct {
	// Environment menjadi komponen pertama state_id dan mengaktifkan state.
	Environment string
	// Extractor dan Loader dikirim sebagai argumen posisional ke `run`.
	Extractor string
	Loader    string
	// RunID dikirim sebagai --run-id. Nilainya juga dipakai sebagai
	// MELTANO_JOB_TRIGGER agar muncul di kolom runs.trigger.
	RunID string
	// TimeoutSeconds menjadi --timeout. Hanya `meltano run` yang punya opsi ini.
	TimeoutSeconds int
	// StateIDSuffix menjadi --state-id-suffix. Tidak boleh mengandung ":".
	StateIDSuffix string
	// FullRefresh dan RefreshCatalog memetakan ke opsi bernama sama.
	FullRefresh    bool
	RefreshCatalog bool
	// LogFormat kosong berarti LogFormatJSON.
	LogFormat string
}

// BuildArgs menyusun argv untuk satu `meltano run`.
//
// Urutannya penting dan mencerminkan implementasi Click di Meltano:
// opsi tingkat grup (--environment, --log-format) HARUS mendahului subcommand,
// sedangkan opsi milik subcommand (--run-id, --timeout, ...) menyusul setelahnya.
// Daftar opsi run yang sah: src/meltano/cli/run.py:48-130.
//
// --no-install selalu disertakan: default auto_install adalah true
// (src/meltano/core/bundle/settings.yml:52-55), dan membiarkan instalasi plugin
// terjadi di jalur request adalah hal yang harus dihindari.
func BuildArgs(spec RunSpec) []string {
	args := make([]string, 0, 12)

	// --- opsi grup ---
	if spec.Environment != "" {
		args = append(args, "--environment="+spec.Environment)
	}
	format := spec.LogFormat
	if format == "" {
		format = LogFormatJSON
	}
	args = append(args, "--log-format="+format)

	// --- subcommand + argumen posisional ---
	args = append(args, "run", spec.Extractor, spec.Loader)

	// --- opsi milik `run` ---
	if spec.StateIDSuffix != "" {
		args = append(args, "--state-id-suffix="+spec.StateIDSuffix)
	}
	if spec.RunID != "" {
		args = append(args, "--run-id="+spec.RunID)
	}
	if spec.TimeoutSeconds > 0 {
		args = append(args, "--timeout="+strconv.Itoa(spec.TimeoutSeconds))
	}
	if spec.FullRefresh {
		args = append(args, "--full-refresh")
	}
	if spec.RefreshCatalog {
		args = append(args, "--refresh-catalog")
	}
	args = append(args, "--no-install")

	return args
}
