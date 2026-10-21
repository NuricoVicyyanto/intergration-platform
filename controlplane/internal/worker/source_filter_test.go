package worker

import (
	"encoding/json"
	"testing"

	"github.com/example/controlplane/internal/domain"
)

func TestApplySourceFilterMySQLStreamMap(t *testing.T) {
	env := map[string]string{}
	err := applySourceFilter(env, "tap-mysql", domain.PipelineSourceFilter{
		Streams:   []string{"appdb-employees", "appdb-orders"},
		RowFilter: `status == "active"`,
	})
	if err != nil {
		t.Fatalf("applySourceFilter: %v", err)
	}
	var streamMaps map[string]any
	if err := json.Unmarshal([]byte(env["TAP_MYSQL_STREAM_MAPS"]), &streamMaps); err != nil {
		t.Fatalf("stream maps bukan JSON: %v", err)
	}
	if streamMaps["__else__"] != nil {
		t.Fatalf("__else__ = %#v, ingin null", streamMaps["__else__"])
	}
	selected, _ := streamMaps["appdb-employees"].(map[string]any)
	if selected["__filter__"] != `status == "active"` {
		t.Fatalf("filter = %#v", selected)
	}
}

func TestApplySourceFilterOracleTables(t *testing.T) {
	env := map[string]string{}
	err := applySourceFilter(env, "tap-oracle", domain.PipelineSourceFilter{
		Streams: []string{"APP.EMPLOYEES", "APP.DEPARTMENTS"},
	})
	if err != nil {
		t.Fatalf("applySourceFilter: %v", err)
	}
	if got := env["MELTANO_EXTRACT__SELECT"]; got != `["APP-EMPLOYEES","APP-DEPARTMENTS"]` {
		t.Fatalf("MELTANO_EXTRACT__SELECT = %s", got)
	}
	if got := env["TAP_ORACLE_FILTER_SCHEMAS"]; got != "APP" {
		t.Fatalf("filter schemas = %q", got)
	}
}

func TestApplySourceFilterOracleRejectsRowFilter(t *testing.T) {
	err := applySourceFilter(map[string]string{}, "tap-oracle", domain.PipelineSourceFilter{
		Streams:   []string{"APP.EMPLOYEES"},
		RowFilter: `status == "active"`,
	})
	if err == nil {
		t.Fatal("filter baris Oracle harus ditolak")
	}
}
