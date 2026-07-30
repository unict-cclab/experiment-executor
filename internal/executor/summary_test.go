package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/unict-cclab/experiment-executor/internal/config"
)

func TestAggregateRunSummariesIncludesSLOViolationPercentage(t *testing.T) {
	dir := t.TempDir()
	experiment := &config.Experiment{Name: "test", Runs: 2, SourceDir: dir}
	for index, violationPercentage := range []float64{25, 75} {
		runDir := filepath.Join(
			experiment.RunsDir(), formatRunID(index+1), "load-gen",
		)
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(map[string]any{
			"response_time_ms": map[string]any{
				"slo_violation_pct": violationPercentage,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(runDir, "summary.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := aggregateRunSummaries(experiment); err != nil {
		t.Fatalf("aggregateRunSummaries() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary experimentSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	if got := summary.Metrics["response_time_ms.slo_violation_pct"]; got != 50 {
		t.Fatalf("SLO violation percentage = %v, want 50", got)
	}
}
