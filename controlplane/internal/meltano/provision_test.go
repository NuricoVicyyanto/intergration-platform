package meltano

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProvisionEnvIsWritableAndUTF8(t *testing.T) {
	t.Setenv("MELTANO_PROJECT_READONLY", "true")
	t.Setenv("MELTANO_AUTO_INSTALL", "false")
	t.Setenv("PYTHONUTF8", "0")

	env := provisionEnv()
	values := make(map[string]string, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	if _, ok := values["MELTANO_PROJECT_READONLY"]; ok {
		t.Fatal("MELTANO_PROJECT_READONLY tidak boleh diteruskan saat provisioning")
	}
	if _, ok := values["MELTANO_AUTO_INSTALL"]; ok {
		t.Fatal("MELTANO_AUTO_INSTALL tidak boleh diteruskan saat provisioning")
	}
	if values["PYTHONUTF8"] != "1" {
		t.Fatalf("PYTHONUTF8 = %q, ingin 1", values["PYTHONUTF8"])
	}
}

func TestAppendDatabaseURI(t *testing.T) {
	got := appendDatabaseURI([]string{"install", "tap-postgres"}, "postgresql://db/system")
	want := []string{
		"install",
		"tap-postgres",
		"--database-uri",
		"postgresql://db/system",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args = %#v, ingin %#v", got, want)
	}
	if args := appendDatabaseURI([]string{"install"}, " "); len(args) != 1 {
		t.Fatalf("URI kosong tidak boleh ditambahkan: %#v", args)
	}
}

func TestPatchTargetMSSQL(t *testing.T) {
	projectDir := t.TempDir()
	targetFile := filepath.Join(
		projectDir,
		".meltano", "loaders", "target-mssql", "venv",
		"lib", "python3.13", "site-packages", "target_mssql", "target.py",
	)
	if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
		t.Fatal(err)
	}
	original := `th.Property(
                    "path_prefix",
                    th.StringType,
                    default="target-mssql",
                    description="Path prefix",
                ),`
	if err := os.WriteFile(targetFile, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	lockFile := filepath.Join(
		projectDir, "plugins", "loaders", "target-mssql--meltanolabs.lock",
	)
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := `{
  "settings_group_validation": [
    [
      "azure_blob_storage.account_name",
      "azure_blob_storage.container",
      "azure_blob_storage.credential",
      "azure_blob_storage.data_source",
      "azure_blob_storage.sas_token"
    ]
  ],
  "settings": [
    {
      "name": "azure_blob_storage.path_prefix",
      "kind": "string",
      "value": "target-mssql"
    },
    {
      "name": "host",
      "kind": "string"
    }
  ]
}`
	if err := os.WriteFile(lockFile, []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := patchTargetMSSQL(projectDir); err != nil {
		t.Fatalf("patch: %v", err)
	}
	patched, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(patched), `default="target-mssql"`) {
		t.Fatalf("default Azure path masih ada:\n%s", patched)
	}
	patchedLock, err := os.ReadFile(lockFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(patchedLock), "azure_blob_storage.path_prefix") {
		t.Fatalf("default Azure lock masih ada:\n%s", patchedLock)
	}
	if strings.Contains(string(patchedLock), "azure_blob_storage.sas_token") {
		t.Fatalf("required Azure group masih ada:\n%s", patchedLock)
	}

	// Re-running the patch must be safe after plugin installation is repeated.
	if err := patchTargetMSSQL(projectDir); err != nil {
		t.Fatalf("patch kedua: %v", err)
	}
}

func TestPreferredProvisionOptions(t *testing.T) {
	mysqlTap := preferredProvisionOptions("tap-mysql")
	if mysqlTap.Variant != "meltanolabs" {
		t.Fatalf("tap-mysql variant = %q", mysqlTap.Variant)
	}
	mysqlTarget := preferredProvisionOptions("target-mysql")
	if mysqlTarget.Python != "3.11" {
		t.Fatalf("target-mysql python = %q", mysqlTarget.Python)
	}
	if got := preferredProvisionOptions("tap-mongodb").Variant; got != "meltanolabs" {
		t.Fatalf("tap-mongodb variant = %q", got)
	}
	if got := preferredProvisionOptions("target-mongodb").Variant; got != "hotgluexyz" {
		t.Fatalf("target-mongodb variant = %q", got)
	}
	if got := preferredProvisionOptions("tap-rest-api-msdk").Variant; got != "widen" {
		t.Fatalf("tap-rest-api-msdk variant = %q", got)
	}
	if got := preferredProvisionOptions("target-s3"); got.Python != "3.10" || got.Variant != "crowemi" {
		t.Fatalf("target-s3 options = %#v", got)
	}
	if got := preferredProvisionOptions("tap-oracle").Python; got != "3.10" {
		t.Fatalf("tap-oracle python = %q", got)
	}
	if got := preferredProvisionOptions("target-oracle").Python; got != "3.10" {
		t.Fatalf("target-oracle python = %q", got)
	}
	if got := preferredProvisionOptions("target-mssql"); got != (provisionOptions{}) {
		t.Fatalf("target-mssql options = %#v", got)
	}
}
