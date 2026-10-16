package meltano

import (
	"testing"

	"github.com/example/controlplane/internal/domain"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   int
		logTail    string
		wantStatus domain.RunStatus
		wantRetry  bool
	}{
		{"sukses", 0, "", domain.StatusSucceeded, false},
		{"gagal biasa", 1, "Extractor failed", domain.StatusFailed, true},
		{
			"timeout terdeteksi dari log", 1,
			`{"event": "Run timed out", "timeout_seconds": 60}`,
			domain.StatusTimedOut, true,
		},
		{"versi tidak cocok", 3, "", domain.StatusFailed, false},
		{"sigterm", 143, "", domain.StatusCancelled, false},
		{"kode tak dikenal", 7, "", domain.StatusFailed, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.exitCode, tc.logTail)
			if got.Status != tc.wantStatus {
				t.Fatalf("status = %q, mau %q", got.Status, tc.wantStatus)
			}
			if got.Retryable != tc.wantRetry {
				t.Fatalf("retryable = %v, mau %v", got.Retryable, tc.wantRetry)
			}
			// Reason wajib ada untuk setiap hasil selain sukses.
			if tc.wantStatus != domain.StatusSucceeded && got.Reason == "" {
				t.Fatal("Reason tidak boleh kosong untuk hasil non-sukses")
			}
		})
	}
}
