// Command provision menyiapkan project Meltano dan plugin untuk satu tenant.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/controlplane/internal/config"
	"github.com/example/controlplane/internal/meltano"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	tenant := flag.String("tenant", "", "nama tenant; project memakai CP_TENANTS_ROOT/<tenant>")
	projectDir := flag.String("project-dir", "", "path project Meltano eksplisit")
	extractor := flag.String("extractor", "tap-postgres", "nama extractor Meltano")
	loader := flag.String("loader", "target-postgres", "nama loader Meltano")
	databaseURI := flag.String("database-uri", cfg.SystemDBURI(), "system database Meltano")
	flag.Parse()

	dir := strings.TrimSpace(*projectDir)
	if dir == "" {
		name := strings.TrimSpace(*tenant)
		if name == "" {
			return fmt.Errorf("isi -project-dir atau -tenant")
		}
		if filepath.Base(name) != name || name == "." {
			return fmt.Errorf("nama tenant tidak valid: %q", name)
		}
		if strings.TrimSpace(cfg.TenantsRoot) == "" {
			return fmt.Errorf("CP_TENANTS_ROOT kosong; isi -project-dir secara eksplisit")
		}
		dir = filepath.Join(cfg.TenantsRoot, name)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve project dir: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	adapter := &meltano.Adapter{Bin: cfg.MeltanoBin}
	plugins, err := adapter.ProvisionProject(ctx, meltano.ProvisionRequest{
		ProjectDir: abs,
		Extractor:  *extractor,
		Loader:     *loader,
		SystemDB:   *databaseURI,
	})
	if err != nil {
		return err
	}

	fmt.Printf("project: %s\n", abs)
	for _, plugin := range plugins {
		if plugin.Name == *extractor || plugin.Name == *loader {
			fmt.Printf("%s: %s (%s)\n", plugin.Type, plugin.Name, plugin.Variant)
		}
	}
	return nil
}
