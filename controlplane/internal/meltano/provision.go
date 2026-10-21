package meltano

import (
	"context"
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
		return provisionOptions{Variant: "hotgluexyz"}
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
