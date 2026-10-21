package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/example/controlplane/internal/domain"
	"github.com/example/controlplane/internal/store"
)

func TestGetRunLogsReturnsProgressAndRecords(t *testing.T) {
	srv, st := newTestServer(nil, "")
	tenant := createTenant(t, st, "acme")
	source := createConnection(t, st, tenant.ID, "mysql", domain.KindSource, domain.DriverMySQL)
	destination := createConnection(t, st, tenant.ID, "postgres", domain.KindDestination, domain.DriverPostgres)
	pipeline, err := st.CreatePipeline(context.Background(), domain.Pipeline{
		TenantID:                tenant.ID,
		Name:                    "mysql-to-postgres",
		Extractor:               "tap-mysql",
		Loader:                  "target-postgres",
		SourceConnectionID:      source.ID,
		DestinationConnectionID: destination.ID,
		DefaultTimeoutSeconds:   60,
		MaxAttempts:             1,
		Enabled:                 true,
	})
	if err != nil {
		t.Fatalf("CreatePipeline: %v", err)
	}

	run, err := st.CreateRun(context.Background(), domain.Run{
		PipelineID:     pipeline.ID,
		Status:         domain.StatusQueued,
		Trigger:        "manual",
		TimeoutSeconds: 60,
	}, "run-logs-test")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	claimed, err := st.ClaimRun(context.Background(), "worker-test", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimRun: %v, %#v", err, claimed)
	}

	logTail := strings.Join([]string{
		`{"event":"Running catalog discovery","name":"tap-mysql","timestamp":"2026-10-21T04:05:03Z"}`,
		`{"event":"Beginning full_table sync of 'employees'...","name":"tap-mysql","timestamp":"2026-10-21T04:05:05Z"}`,
		`{"event":"METRIC: {\"type\":\"counter\",\"metric\":\"record_count\",\"value\":42,\"tags\":{\"stream\":\"employees\"}}","name":"tap-mysql","timestamp":"2026-10-21T04:05:06Z"}`,
		`{"event":"METRIC: {\"type\":\"counter\",\"metric\":\"record_count\",\"value\":42,\"tags\":{\"stream\":\"employees\"}}","name":"target-postgres","timestamp":"2026-10-21T04:05:07Z"}`,
		`{"event":"Run completed","name":"meltano","timestamp":"2026-10-21T04:05:08Z"}`,
	}, "\n")
	exitCode := 0
	if _, err := st.CompleteRun(context.Background(), run.ID, store.Completion{
		Status:   domain.StatusSucceeded,
		ExitCode: &exitCode,
		LogTail:  logTail,
	}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	rec := doGet(t, srv, "/v1/runs/"+run.ID+"/logs")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, ingin 200 (%s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	summary, ok := body["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary tidak ada: %#v", body)
	}
	if summary["stage"] != "completed" {
		t.Fatalf("stage = %v", summary["stage"])
	}
	if summary["records_extracted"] != float64(42) || summary["records_loaded"] != float64(42) {
		t.Fatalf("record counts = %#v", summary)
	}
	streams, _ := summary["streams"].([]any)
	if len(streams) != 1 {
		t.Fatalf("streams = %#v", streams)
	}
	logData, _ := body["log"].(map[string]any)
	lines, _ := logData["lines"].([]any)
	if len(lines) != 5 {
		t.Fatalf("log lines = %d, ingin 5", len(lines))
	}
}
