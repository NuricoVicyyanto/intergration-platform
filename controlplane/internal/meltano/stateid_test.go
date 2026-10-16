package meltano

import "testing"

func TestStateID(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		extractor   string
		loader      string
		suffix      string
		want        string
		wantErr     bool
	}{
		{
			name: "format dasar", environment: "dev",
			extractor: "tap-postgres", loader: "target-postgres",
			want: "dev:tap-postgres-to-target-postgres",
		},
		{
			name: "dengan suffix", environment: "dev",
			extractor: "tap-postgres", loader: "target-postgres", suffix: "backfill-2026-10",
			want: "dev:tap-postgres-to-target-postgres:backfill-2026-10",
		},
		{
			name:      "environment kosong dilewati",
			extractor: "tap-x", loader: "target-y",
			want: "tap-x-to-target-y",
		},
		{
			name: "suffix kosong dilewati", environment: "prod",
			extractor: "tap-x", loader: "target-y", suffix: "",
			want: "prod:tap-x-to-target-y",
		},
		{
			name: "suffix mengandung titik dua ditolak", environment: "dev",
			extractor: "tap-x", loader: "target-y", suffix: "a:b",
			wantErr: true,
		},
		{
			name: "extractor mengandung titik dua ditolak", environment: "dev",
			extractor: "tap:x", loader: "target-y",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StateID(tc.environment, tc.extractor, tc.loader, tc.suffix)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("mau error, dapat %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("error tak terduga: %v", err)
			}
			if got != tc.want {
				t.Fatalf("dapat %q, mau %q", got, tc.want)
			}
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// Kasus nyata pada mesin Windows, diverifikasi terhadap
			// <user_log_path>\meltano\Logs\elt\devtap-gitlab-to-target-jsonl
			name: "titik dua dihapus seperti perilaku Meltano",
			in:   "dev:tap-gitlab-to-target-jsonl",
			want: "devtap-gitlab-to-target-jsonl",
		},
		{name: "spasi menjadi underscore", in: "my env", want: "my_env"},
		{name: "rentetan spasi menjadi satu underscore", in: "a   b", want: "a_b"},
		{name: "non-ascii dibuang", in: "caf\u00e9", want: "caf"},
		{name: "garis miring tidak bisa keluar folder", in: "../../etc/passwd", want: "etc_passwd"},
		{name: "titik dan underscore di ujung dipangkas", in: "._name._", want: "name"},
		{name: "nama reserved Windows diberi awalan", in: "CON", want: "_CON"},
		{name: "nama reserved dengan ekstensi", in: "nul.txt", want: "_nul.txt"},
		{name: "hanya karakter terlarang", in: "***", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeFilename(tc.in); got != tc.want {
				t.Fatalf("SanitizeFilename(%q) = %q, mau %q", tc.in, got, tc.want)
			}
		})
	}
}
