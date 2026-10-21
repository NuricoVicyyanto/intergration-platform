package meltano

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/example/controlplane/internal/domain"
)

// Plugin adalah satu baris keluaran `meltano plugin list --format=json`.
//
// Field-nya mengikuti dataclass PluginListing di src/meltano/cli/plugin.py.
type Plugin struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Variant     string `json:"variant"`
	InheritFrom string `json:"inherit_from"`
	Custom      bool   `json:"custom"`
	Update      bool   `json:"update"`
	PipURL      string `json:"pip_url"`
}

// ListPlugins menjalankan `meltano plugin list --format=json` di projectDir.
//
// Perintah ini hanya membaca project (tidak menyentuh state atau jaringan pada
// kondisi normal: pengecekan update hanya aktif bila login Meltano Cloud ada),
// jadi aman dipanggil dari jalur request GET.
func (a *Adapter) ListPlugins(ctx context.Context, projectDir string) ([]Plugin, error) {
	if strings.TrimSpace(projectDir) == "" {
		return nil, fmt.Errorf("%w: ProjectDir wajib diisi", domain.ErrInvalid)
	}
	bin := a.Bin
	if bin == "" {
		bin = "meltano"
	}

	cmd := exec.CommandContext(ctx, bin, "plugin", "list", "--format=json")
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Keluaran JSON hanya ditulis bila perintah sukses, jadi stderr
			// adalah satu-satunya petunjuk penyebab kegagalan.
			return nil, fmt.Errorf("meltano plugin list gagal (exit %d): %s",
				exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("menjalankan meltano plugin list: %w", err)
	}

	var plugins []Plugin
	if err := json.Unmarshal(out, &plugins); err != nil {
		return nil, fmt.Errorf("membaca keluaran meltano plugin list: %w", err)
	}
	if a.Logger != nil {
		a.Logger.Debug("meltano plugin list",
			"project_dir", projectDir, "plugins", len(plugins))
	}
	return plugins, nil
}
