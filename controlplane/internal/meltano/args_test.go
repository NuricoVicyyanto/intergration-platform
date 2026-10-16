package meltano

import (
	"reflect"
	"testing"
)

func TestBuildArgsUrutanOpsiGrupMendahuluiSubcommand(t *testing.T) {
	got := BuildArgs(RunSpec{
		Environment:    "dev",
		Extractor:      "tap-postgres",
		Loader:         "target-postgres",
		RunID:          "11111111-1111-4111-8111-111111111111",
		TimeoutSeconds: 3600,
	})
	want := []string{
		"--environment=dev",
		"--log-format=json",
		"run",
		"tap-postgres",
		"target-postgres",
		"--run-id=11111111-1111-4111-8111-111111111111",
		"--timeout=3600",
		"--no-install",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv tidak sesuai:\n dapat %v\n mau   %v", got, want)
	}
}

func TestBuildArgsOpsiOpsional(t *testing.T) {
	got := BuildArgs(RunSpec{
		Environment:    "acme",
		Extractor:      "tap-x",
		Loader:         "target-y",
		StateIDSuffix:  "backfill",
		FullRefresh:    true,
		RefreshCatalog: true,
	})
	want := []string{
		"--environment=acme",
		"--log-format=json",
		"run", "tap-x", "target-y",
		"--state-id-suffix=backfill",
		"--full-refresh",
		"--refresh-catalog",
		"--no-install",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv tidak sesuai:\n dapat %v\n mau   %v", got, want)
	}
}

func TestBuildArgsTanpaEnvironment(t *testing.T) {
	// --environment boleh absen hanya bila meltano.yml punya default_environment.
	// Kalau keduanya tidak ada, `meltano run` gagal karena environment_required.
	got := BuildArgs(RunSpec{Extractor: "tap-x", Loader: "target-y"})
	want := []string{"--log-format=json", "run", "tap-x", "target-y", "--no-install"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv tidak sesuai:\n dapat %v\n mau   %v", got, want)
	}
}

func TestBuildArgsSelaluNoInstall(t *testing.T) {
	for _, spec := range []RunSpec{
		{},
		{Extractor: "a", Loader: "b"},
		{Environment: "e", Extractor: "a", Loader: "b", FullRefresh: true},
	} {
		args := BuildArgs(spec)
		found := false
		for _, a := range args {
			if a == "--no-install" {
				found = true
			}
		}
		if !found {
			t.Fatalf("--no-install wajib selalu ada, dapat %v", args)
		}
	}
}
