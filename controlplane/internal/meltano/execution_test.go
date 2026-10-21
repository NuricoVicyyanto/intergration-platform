package meltano

import (
	"testing"
	"time"

	"github.com/example/controlplane/internal/domain"
)

func TestSummarizeExecutionLog(t *testing.T) {
	started := time.Date(2026, 10, 21, 4, 5, 3, 0, time.UTC)
	ended := started.Add(24 * time.Second)
	run := domain.Run{
		Status:    domain.StatusSucceeded,
		CreatedAt: started.Add(-2 * time.Second),
		StartedAt: &started,
		EndedAt:   &ended,
	}
	rawLines := []string{
		`{"event":"Running catalog discovery","name":"tap-mysql","level":"info","timestamp":"2026-10-21T04:05:03Z"}`,
		`{"event":"Beginning full_table sync of 'appdb-employees'...","name":"tap-mysql","level":"info","timestamp":"2026-10-21T04:05:05Z"}`,
		`{"event":"METRIC: {\"type\":\"counter\",\"metric\":\"record_count\",\"value\":1010,\"tags\":{\"stream\":\"appdb-employees\"}}","name":"tap-mysql","level":"info","timestamp":"2026-10-21T04:05:06Z"}`,
		`{"event":"Reader 'target-postgres' completed processing 1012 lines of input (1 schemas, 1010 records, 0 batch manifests, 1 state messages).","name":"target-postgres","level":"info","timestamp":"2026-10-21T04:05:07Z"}`,
		`{"event":"METRIC: {\"type\":\"counter\",\"metric\":\"record_count\",\"value\":1010,\"tags\":{\"stream\":\"appdb-employees\"}}","name":"target-postgres","level":"info","timestamp":"2026-10-21T04:05:08Z"}`,
		`{"event":"Run completed","name":"meltano","level":"info","timestamp":"2026-10-21T04:05:27Z"}`,
	}
	lines := make([]ExecutionLogLine, 0, len(rawLines))
	for _, raw := range rawLines {
		lines = append(lines, ParseExecutionLine(raw))
	}

	got := SummarizeExecutionLog(lines, "tap-mysql", "target-postgres", run)
	if got.Stage != "completed" || got.ProgressPercent != 100 {
		t.Fatalf("stage/progress = %s/%d", got.Stage, got.ProgressPercent)
	}
	if got.RecordsExtracted == nil || *got.RecordsExtracted != 1010 {
		t.Fatalf("records extracted = %v", got.RecordsExtracted)
	}
	if got.RecordsLoaded == nil || *got.RecordsLoaded != 1010 {
		t.Fatalf("records loaded = %v", got.RecordsLoaded)
	}
	if len(got.Streams) != 1 || got.Streams[0].Name != "appdb-employees" {
		t.Fatalf("streams = %#v", got.Streams)
	}
	if got.Streams[0].RecordsExtracted == nil || *got.Streams[0].RecordsExtracted != 1010 ||
		got.Streams[0].RecordsLoaded == nil || *got.Streams[0].RecordsLoaded != 1010 {
		t.Fatalf("stream counts = %#v", got.Streams[0])
	}
	if got.Steps[len(got.Steps)-1].Status != "completed" {
		t.Fatalf("step terakhir = %#v", got.Steps[len(got.Steps)-1])
	}
}

func TestSummarizeExecutionQueued(t *testing.T) {
	run := domain.Run{
		Status:    domain.StatusQueued,
		CreatedAt: time.Now().UTC(),
	}
	got := SummarizeExecutionLog(nil, "tap-mysql", "target-postgres", run)
	if got.Stage != "queued" || got.Steps[0].Status != "active" {
		t.Fatalf("summary queued = %#v", got)
	}
}
