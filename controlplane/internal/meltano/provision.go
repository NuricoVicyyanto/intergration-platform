package meltano

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/example/controlplane/internal/domain"
)

// ProvisionRequest describes a one-time Meltano project setup for a tenant.
//
// Provisioning is intentionally separate from Run: it may access PyPI and write
// meltano.yml or plugin virtualenvs, while runtime execution is read-only and
// always uses --no-install.
type ProvisionRequest struct {
	ProjectDir string
	Extractor  string
	Loader     string
	SystemDB   string
}

// ProvisionProject creates a Meltano project when needed, adds the requested
// connector plugins, and installs their dependencies.
func (a *Adapter) ProvisionProject(
	ctx context.Context,
	req ProvisionRequest,
) ([]Plugin, error) {
	if strings.TrimSpace(req.ProjectDir) == "" {
		return nil, fmt.Errorf("%w: ProjectDir wajib diisi", domain.ErrInvalid)
	}
	if strings.TrimSpace(req.Extractor) == "" || strings.TrimSpace(req.Loader) == "" {
		return nil, fmt.Errorf("%w: Extractor dan Loader wajib diisi", domain.ErrInvalid)
	}
	if filepath.Base(req.Extractor) != req.Extractor ||
		filepath.Base(req.Loader) != req.Loader {
		return nil, fmt.Errorf("%w: nama plugin tidak boleh mengandung path", domain.ErrInvalid)
	}

	if _, err := os.Stat(filepath.Join(req.ProjectDir, "meltano.yml")); errors.Is(err, os.ErrNotExist) {
		if err := a.InitProject(ctx, req.ProjectDir); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("periksa project %s: %w", req.ProjectDir, err)
	}

	current, err := a.ListPlugins(ctx, req.ProjectDir)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(current))
	for _, plugin := range current {
		present[plugin.Name] = true
	}
	missing := make([]string, 0, 2)
	for _, name := range []string{req.Extractor, req.Loader} {
		if !present[name] {
			missing = append(missing, name)
		}
	}
	for _, name := range missing {
		options := preferredProvisionOptions(name)
		args := []string{"add", name}
		if options.Variant != "" {
			args = append(args, "--variant", options.Variant)
		}
		if options.Python != "" {
			args = append(args, "--python", options.Python)
		}
		args = append(args, "--no-install")
		args = appendDatabaseURI(args, req.SystemDB)
		if _, err := a.runProvisionCommand(ctx, req.ProjectDir, args...); err != nil {
			return nil, err
		}
	}

	args := []string{"install", req.Extractor, req.Loader}
	args = appendDatabaseURI(args, req.SystemDB)
	if _, err := a.runProvisionCommand(ctx, req.ProjectDir, args...); err != nil {
		return nil, err
	}
	if req.Loader == "target-mssql" {
		if err := patchTargetMSSQL(req.ProjectDir); err != nil {
			return nil, err
		}
	}

	installed, err := a.ListPlugins(ctx, req.ProjectDir)
	if err != nil {
		return nil, err
	}
	present = make(map[string]bool, len(installed))
	for _, plugin := range installed {
		present[plugin.Name] = true
	}
	for _, name := range []string{req.Extractor, req.Loader} {
		if !present[name] {
			return nil, fmt.Errorf("plugin %s tidak ada di project setelah provisioning", name)
		}
	}
	return installed, nil
}

func patchTargetMSSQL(projectDir string) error {
	if err := patchTargetMSSQLOptionalAzure(projectDir); err != nil {
		return err
	}
	return patchTargetMSSQLLockfile(projectDir)
}

// patchTargetMSSQLOptionalAzure fixes an upstream schema bug where the
// optional Azure staging object is always materialized because its nested
// path_prefix has a default. The result is a non-empty object without the
// required Azure fields, so configurations that do not use Azure fail
// validation. Patch after every install because reinstalling restores the file.
func patchTargetMSSQLOptionalAzure(projectDir string) error {
	root := filepath.Join(projectDir, ".meltano", "loaders", "target-mssql")
	targetFile := ""
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "target.py" {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) != "target_mssql" {
			return nil
		}
		targetFile = path
		return filepath.SkipAll
	})
	if err != nil {
		return fmt.Errorf("cari schema target-mssql: %w", err)
	}
	if targetFile == "" {
		return fmt.Errorf("schema target-mssql tidak ditemukan setelah instalasi")
	}

	info, err := os.Stat(targetFile)
	if err != nil {
		return fmt.Errorf("periksa schema target-mssql: %w", err)
	}
	content, err := os.ReadFile(targetFile)
	if err != nil {
		return fmt.Errorf("baca schema target-mssql: %w", err)
	}

	const azurePathDefault = "                    default=\"target-mssql\",\n"
	if !strings.Contains(string(content), azurePathDefault) {
		return nil
	}
	patched := strings.ReplaceAll(string(content), azurePathDefault, "")
	if err := os.WriteFile(targetFile, []byte(patched), info.Mode()); err != nil {
		return fmt.Errorf("patch schema target-mssql: %w", err)
	}
	return nil
}

// patchTargetMSSQLLockfile removes the Azure-only required group and nested
// default from the Hub lockfile. Meltano uses the lockfile for config
// composition, so patching only the installed Python package is insufficient.
func patchTargetMSSQLLockfile(projectDir string) error {
	files, err := filepath.Glob(filepath.Join(
		projectDir, "plugins", "loaders", "target-mssql*.lock",
	))
	if err != nil {
		return fmt.Errorf("cari lockfile target-mssql: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("lockfile target-mssql tidak ditemukan")
	}

	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("baca lockfile target-mssql: %w", err)
		}
		var definition map[string]any
		if err := json.Unmarshal(content, &definition); err != nil {
			return fmt.Errorf("parse lockfile target-mssql: %w", err)
		}

		changed := false
		if groups, ok := definition["settings_group_validation"].([]any); ok {
			filtered := make([]any, 0, len(groups))
			for _, group := range groups {
				items, _ := group.([]any)
				hasAzure := false
				for _, item := range items {
					name, _ := item.(string)
					if strings.HasPrefix(name, "azure_blob_storage.") {
						hasAzure = true
						break
					}
				}
				if hasAzure {
					changed = true
					continue
				}
				filtered = append(filtered, group)
			}
			definition["settings_group_validation"] = filtered
		}

		if settings, ok := definition["settings"].([]any); ok {
			filtered := make([]any, 0, len(settings))
			for _, setting := range settings {
				item, _ := setting.(map[string]any)
				if item["name"] == "azure_blob_storage.path_prefix" {
					changed = true
					continue
				}
				filtered = append(filtered, setting)
			}
			definition["settings"] = filtered
		}

		if !changed {
			continue
		}
		patched, err := json.MarshalIndent(definition, "", "  ")
		if err != nil {
			return fmt.Errorf("encode lockfile target-mssql: %w", err)
		}
		patched = append(patched, '\n')
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("periksa lockfile target-mssql: %w", err)
		}
		if err := os.WriteFile(path, patched, info.Mode()); err != nil {
			return fmt.Errorf("patch lockfile target-mssql: %w", err)
		}
	}
	return nil
}

type provisionOptions struct {
	Variant string
	Python  string
}

// preferredProvisionOptions selects compatible community variants or a plugin
// Python version where the default Hub definition is legacy-only.
func preferredProvisionOptions(name string) provisionOptions {
	switch name {
	case "tap-mysql":
		return provisionOptions{Variant: "meltanolabs"}
	case "tap-mongodb":
		return provisionOptions{Variant: "meltanolabs"}
	case "tap-rest-api-msdk":
		return provisionOptions{Variant: "widen"}
	case "tap-csv":
		return provisionOptions{Variant: "meltanolabs"}
	case "tap-google-sheets":
		return provisionOptions{Variant: "matatika"}
	case "target-mysql":
		return provisionOptions{Python: "3.11"}
	case "target-mongodb":
		return provisionOptions{Variant: "hotgluexyz", Python: "3.11"}
	case "target-s3":
		return provisionOptions{Variant: "crowemi", Python: "3.10"}
	case "target-duckdb":
		return provisionOptions{Variant: "meltanolabs"}
	case "tap-oracle", "target-oracle":
		// cx_Oracle 8.3, used by these legacy plugins, has a binary wheel for
		// CPython 3.10. Python 3.11 falls back to a source build that requires
		// Visual C++ Build Tools on Windows.
		return provisionOptions{Python: "3.10"}
	default:
		return provisionOptions{}
	}
}

func appendDatabaseURI(args []string, databaseURI string) []string {
	if strings.TrimSpace(databaseURI) == "" {
		return args
	}
	return append(args, "--database-uri", databaseURI)
}

func (a *Adapter) runProvisionCommand(
	ctx context.Context,
	projectDir string,
	args ...string,
) ([]byte, error) {
	bin := a.Bin
	if bin == "" {
		bin = "meltano"
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = projectDir
	cmd.Env = provisionEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("meltano %s gagal: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

// provisionEnv deliberately removes runtime-only read-only/auto-install flags.
// Provisioning is the supported place where project files and venvs may change.
func provisionEnv() []string {
	skip := map[string]bool{
		"MELTANO_PROJECT_READONLY": true,
		"MELTANO_AUTO_INSTALL":     true,
		"NO_COLOR":                 true,
		"TERM":                     true,
		"PYTHONUTF8":               true,
	}
	out := make([]string, 0, len(os.Environ())+4)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !skip[key] {
			out = append(out, item)
		}
	}
	return append(out,
		"NO_COLOR=1",
		"TERM=dumb",
		"PYTHONUTF8=1",
		"MELTANO_SEND_ANONYMOUS_USAGE_STATS=false",
	)
}
