package worker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/controlplane/internal/domain"
)

// applySourceFilter menerjemahkan pilihan pipeline menjadi environment yang
// dibaca plugin Meltano. Stream map modern memilih stream dan menyaring record;
// tap-oracle legacy memakai filter_schemas/filter_tables saat discovery.
func applySourceFilter(
	env map[string]string,
	extractor string,
	filter domain.PipelineSourceFilter,
) error {
	if len(filter.Streams) == 0 && strings.TrimSpace(filter.RowFilter) == "" {
		return nil
	}

	base, _, _ := strings.Cut(extractor, "--")
	if base == "tap-oracle" {
		if strings.TrimSpace(filter.RowFilter) != "" {
			return fmt.Errorf("tap-oracle belum mendukung filter baris")
		}
		return applyOracleTableFilter(env, filter.Streams)
	}

	streams := filter.Streams
	if len(streams) == 0 {
		streams = []string{"*"}
	}
	streamMaps := map[string]any{"__else__": nil}
	for _, stream := range streams {
		stream = strings.TrimSpace(stream)
		if stream == "" {
			continue
		}
		definition := map[string]any{}
		if rowFilter := strings.TrimSpace(filter.RowFilter); rowFilter != "" {
			definition["__filter__"] = rowFilter
		}
		streamMaps[stream] = definition
	}
	encoded, err := json.Marshal(streamMaps)
	if err != nil {
		return fmt.Errorf("encode source filter: %w", err)
	}
	prefix := strings.ToUpper(strings.ReplaceAll(base, "-", "_"))
	env[prefix+"_STREAM_MAPS"] = string(encoded)
	env["MELTANO_EXTRACT_STREAM_MAPS"] = string(encoded)
	return nil
}

func applyOracleTableFilter(env map[string]string, streams []string) error {
	tables := make([]string, 0, len(streams))
	schemas := make([]string, 0, len(streams))
	seenSchemas := map[string]bool{}
	for _, stream := range streams {
		stream = strings.TrimSpace(stream)
		if stream == "" {
			continue
		}
		table := strings.ReplaceAll(stream, ".", "-")
		tables = append(tables, table)
		if i := strings.LastIndex(table, "-"); i > 0 {
			schema := table[:i]
			if !seenSchemas[schema] {
				seenSchemas[schema] = true
				schemas = append(schemas, schema)
			}
		}
	}
	if len(tables) == 0 {
		return nil
	}
	encoded, err := json.Marshal(tables)
	if err != nil {
		return fmt.Errorf("encode filter_tables: %w", err)
	}
	env["TAP_ORACLE_FILTER_TABLES"] = string(encoded)
	env["MELTANO_EXTRACT_FILTER_TABLES"] = string(encoded)
	env["MELTANO_EXTRACT__SELECT"] = string(encoded)
	if len(schemas) > 0 {
		schemaValue := strings.Join(schemas, ",")
		env["TAP_ORACLE_FILTER_SCHEMAS"] = schemaValue
		env["MELTANO_EXTRACT_FILTER_SCHEMAS"] = schemaValue
	}
	return nil
}
