package meltano

import (
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
