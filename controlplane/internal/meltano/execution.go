package meltano

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/example/controlplane/internal/domain"
)

// ExecutionLogLine adalah satu baris output Meltano yang sudah dinormalisasi.
type ExecutionLogLine struct {
	At          *time.Time `json:"at,omitempty"`
	Level       string     `json:"level,omitempty"`
	Message     string     `json:"message"`
	Plugin      string     `json:"plugin,omitempty"`
	Stdio       string     `json:"stdio,omitempty"`
	Stage       string     `json:"stage,omitempty"`
	RecordCount *int64     `json:"record_count,omitempty"`
	Stream      string     `json:"stream,omitempty"`
}

// ExecutionStream merangkum satu stream Singer yang diproses.
type ExecutionStream struct {
	Name             string   `json:"name"`
	Records          int64    `json:"records"`
	RecordsExtracted *int64   `json:"records_extracted,omitempty"`
	RecordsLoaded    *int64   `json:"records_loaded,omitempty"`
	Status           string   `json:"status"`
	DurationSeconds  *float64 `json:"duration_seconds,omitempty"`
}

// ExecutionStep adalah tahap eksekusi yang ditampilkan sebagai progres.
type ExecutionStep struct {
	Key    string     `json:"key"`
	Label  string     `json:"label"`
	Status string     `json:"status"`
	At     *time.Time `json:"at,omitempty"`
}

// ExecutionSummary adalah ringkasan progres yang stabil untuk UI dan callers API.
type ExecutionSummary struct {
	Stage            string            `json:"stage"`
	Message          string            `json:"message"`
	ProgressPercent  int               `json:"progress_percent"`
	RecordsExtracted *int64            `json:"records_extracted,omitempty"`
	RecordsLoaded    *int64            `json:"records_loaded,omitempty"`
	Streams          []ExecutionStream `json:"streams"`
	Steps            []ExecutionStep   `json:"steps"`
}

var processedRecordsPattern = regexp.MustCompile(`(?i)completed processing .*?([0-9]+) records`)

// ParseExecutionLine mengubah satu baris JSON Meltano menjadi data yang aman
// ditampilkan di UI. Baris non-JSON tetap dipertahankan sebagai pesan biasa.
func ParseExecutionLine(raw string) ExecutionLogLine {
	line := ExecutionLogLine{Message: strings.TrimSpace(raw)}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		line.Stage = detectStage(line.Message)
		line.RecordCount, line.Stream = parseRecordMetric(line.Message)
		return line
	}

	line.Level = stringField(payload, "level")
	line.Plugin = firstNonEmpty(stringField(payload, "name"), stringField(payload, "string_id"))
	line.Stdio = stringField(payload, "stdio")
	line.Message = firstNonEmpty(stringField(payload, "event"), stringField(payload, "message"), raw)

	if metric, ok := payload["metric_info"].(map[string]any); ok {
		encoded, _ := json.Marshal(metric)
		line.Message = "METRIC: " + string(encoded)
	}
	if at, err := time.Parse(time.RFC3339Nano, stringField(payload, "timestamp")); err == nil {
		at = at.UTC()
		line.At = &at
	}

	line.Stage = detectStage(line.Message)
	line.RecordCount, line.Stream = parseRecordMetric(line.Message)
	return line
}

// IsUsefulExecutionLine membuang peringatan dependency yang tidak membantu
// operator memahami progres pipeline.
func IsUsefulExecutionLine(line ExecutionLogLine) bool {
	message := strings.ToLower(line.Message)
	if strings.TrimSpace(message) == "" {
		return false
	}
	noise := []string{
		"deprecationwarning",
		"pkg_resources",
		"implementing implicit namespace packages",
		"declare_namespace",
		"__import__(",
		"super().__init__(",
		"skipping parse of env var settings",
	}
	for _, fragment := range noise {
		if strings.Contains(message, fragment) {
			return false
		}
	}
	return line.Stage != "" || line.RecordCount != nil ||
		strings.Contains(message, "meltano") ||
		strings.Contains(message, "environment") ||
		strings.Contains(message, "state") ||
		strings.Contains(message, "target-") ||
		strings.Contains(message, "tap-") ||
		strings.Contains(message, "run completed")
}

// SummarizeExecutionLog membuat progres dari baris log terakhir dan status run.
func SummarizeExecutionLog(
	lines []ExecutionLogLine,
	extractor, loader string,
	run domain.Run,
) ExecutionSummary {
	stages := map[string]*time.Time{}
	streams := map[string]*streamSummary{}
	var extracted, loaded *int64
	message := ""

	for i := range lines {
		line := lines[i]
		if line.At != nil && line.Stage != "" {
			at := *line.At
			stages[line.Stage] = &at
		}
		if line.RecordCount != nil {
			count := *line.RecordCount
			stream := line.Stream
			if stream == "" {
				stream = line.Plugin
			}
			if stream == "" {
				stream = "default"
			}
			current := streams[stream]
			if current == nil {
				current = &streamSummary{name: stream}
				streams[stream] = current
			}
			if count > current.records {
				current.records = count
			}
			switch {
			case line.Plugin == extractor:
				extracted = maxRecordCount(extracted, count)
				current.extracted = maxStreamCount(current.extracted, count)
				current.status = "extracted"
			case line.Plugin == loader:
				loaded = maxRecordCount(loaded, count)
				current.loaded = maxStreamCount(current.loaded, count)
				current.status = "loaded"
			}
		}
		if match := processedRecordsPattern.FindStringSubmatch(line.Message); len(match) == 2 {
			if count, err := strconv.ParseInt(match[1], 10, 64); err == nil {
				loaded = maxRecordCount(loaded, count)
				if len(streams) == 0 {
					loadedCount := count
					streams["default"] = &streamSummary{
						name: "default", records: count, loaded: &loadedCount, status: "loaded",
					}
				}
			}
		}
		if line.Message != "" && line.RecordCount == nil && !isUtilityMessage(line.Message) {
			message = line.Message
		}
	}

	if run.StartedAt != nil {
		starting := *run.StartedAt
		stages["started"] = &starting
	}
	if run.EndedAt != nil && run.Status == domain.StatusSucceeded {
		ended := *run.EndedAt
		stages["completed"] = &ended
	}
	if _, ok := stages["queued"]; !ok {
		queued := run.CreatedAt
		stages["queued"] = &queued
	}

	stage, progress := executionStage(run, stages)
	if run.Status == domain.StatusQueued {
		message = ""
	}
	if message == "" {
		message = stageMessage(stage, run.Error)
	}
	summary := ExecutionSummary{
		Stage:            stage,
		Message:          message,
		ProgressPercent:  progress,
		RecordsExtracted: extracted,
		RecordsLoaded:    loaded,
		Streams:          make([]ExecutionStream, 0, len(streams)),
		Steps:            buildExecutionSteps(stage, stages),
	}
	for _, stream := range streams {
		summary.Streams = append(summary.Streams, ExecutionStream{
			Name:             stream.name,
			Records:          stream.records,
			RecordsExtracted: stream.extracted,
			RecordsLoaded:    stream.loaded,
			Status:           firstNonEmpty(stream.status, "processing"),
		})
	}
	sort.Slice(summary.Streams, func(i, j int) bool {
		return summary.Streams[i].Name < summary.Streams[j].Name
	})
	return summary
}

type streamSummary struct {
	name      string
	records   int64
	extracted *int64
	loaded    *int64
	status    string
}

func detectStage(message string) string {
	normalized := strings.ToLower(message)
	switch {
	case strings.Contains(normalized, "running catalog discovery"):
		return "discovery"
	case strings.Contains(normalized, "beginning ") && strings.Contains(normalized, " sync of "):
		return "extract"
	case strings.Contains(normalized, "inserting with sql"):
		return "load"
	case strings.Contains(normalized, "incremental state has been updated"):
		return "state"
	case strings.Contains(normalized, "run completed"):
		return "completed"
	default:
		return ""
	}
}

func parseRecordMetric(message string) (*int64, string) {
	raw := ""
	if strings.HasPrefix(message, "METRIC:") {
		raw = strings.TrimSpace(strings.TrimPrefix(message, "METRIC:"))
	}
	if raw == "" {
		return nil, ""
	}
	var metric struct {
		Metric string `json:"metric"`
		Value  int64  `json:"value"`
		Tags   struct {
			Stream string `json:"stream"`
		} `json:"tags"`
	}
	if err := json.Unmarshal([]byte(raw), &metric); err != nil {
		return nil, ""
	}
	if metric.Metric != "record_count" {
		return nil, ""
	}
	return &metric.Value, metric.Tags.Stream
}

func executionStage(run domain.Run, stages map[string]*time.Time) (string, int) {
	switch run.Status {
	case domain.StatusQueued:
		return "queued", 3
	case domain.StatusFailed, domain.StatusTimedOut, domain.StatusLost:
		return "failed", 100
	case domain.StatusCancelled:
		return "cancelled", 100
	case domain.StatusSucceeded:
		return "completed", 100
	}
	switch {
	case stages["load"] != nil:
		return "load", 82
	case stages["extract"] != nil:
		return "extract", 58
	case stages["discovery"] != nil:
		return "discovery", 30
	case stages["started"] != nil:
		return "started", 12
	default:
		return "queued", 3
	}
}

func buildExecutionSteps(stage string, stages map[string]*time.Time) []ExecutionStep {
	definitions := []struct {
		key   string
		label string
	}{
		{"queued", "Queued"},
		{"started", "Started"},
		{"discovery", "Discover schema"},
		{"extract", "Extract records"},
		{"load", "Load records"},
		{"completed", "Completed"},
	}
	activeIndex := -1
	for i, definition := range definitions {
		if definition.key == stage {
			activeIndex = i
			break
		}
	}
	if stage == "failed" || stage == "cancelled" {
		for i := len(definitions) - 1; i >= 0; i-- {
			if stages[definitions[i].key] != nil {
				activeIndex = i
				break
			}
		}
	}

	out := make([]ExecutionStep, 0, len(definitions))
	for i, definition := range definitions {
		status := "pending"
		if stages[definition.key] != nil {
			status = "completed"
		}
		if i == activeIndex && stage != "completed" {
			switch stage {
			case "failed":
				status = "failed"
			case "cancelled":
				status = "cancelled"
			default:
				status = "active"
			}
		}
		out = append(out, ExecutionStep{
			Key:    definition.key,
			Label:  definition.label,
			Status: status,
			At:     stages[definition.key],
		})
	}
	return out
}

func stageMessage(stage, runError string) string {
	if runError != "" {
		return runError
	}
	return map[string]string{
		"queued":    "Waiting for a worker to claim this run.",
		"started":   "Meltano process has started.",
		"discovery": "Discovering source streams and schemas.",
		"extract":   "Extracting records from the source.",
		"load":      "Writing records to the destination.",
		"state":     "Updating incremental state.",
		"completed": "Run completed successfully.",
		"failed":    "Run failed.",
		"cancelled": "Run was cancelled.",
	}[stage]
}

func isUtilityMessage(message string) bool {
	normalized := strings.ToLower(message)
	return strings.Contains(normalized, "using systemdb state backend") ||
		strings.Contains(normalized, "reading state from database")
}

func maxRecordCount(current *int64, candidate int64) *int64 {
	if current == nil || candidate > *current {
		value := candidate
		return &value
	}
	return current
}

func maxStreamCount(current *int64, candidate int64) *int64 {
	return maxRecordCount(current, candidate)
}

func stringField(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
