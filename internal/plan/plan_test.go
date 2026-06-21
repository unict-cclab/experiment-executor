package plan

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/unict-cclab/experiment-executor/internal/config"
)

func TestBuildCreatesConfiguredRuns(t *testing.T) {
	experiment := &config.Experiment{
		Name:      "baseline",
		SourceDir: "/workspace/experiments/baseline",
		Runs:      2,
	}

	got := Build(experiment)
	if len(got.Runs) != 2 {
		t.Fatalf("len(Runs) = %d", len(got.Runs))
	}
	if got.Runs[1].ID != "run-002" || got.Runs[1].Number != 2 {
		t.Fatalf("second run = %#v", got.Runs[1])
	}
	wantPath := filepath.Join("/workspace", "experiments", "baseline", "runs", "run-001")
	if got.Runs[0].OutputDir != wantPath {
		t.Fatalf("run output = %q, want %q", got.Runs[0].OutputDir, wantPath)
	}
}

func TestBuildOnlyIncludesEnabledToolPhases(t *testing.T) {
	experiment := &config.Experiment{
		Name:      "candidate",
		SourceDir: "/workspace/experiments/candidate",
		Runs:      1,
		Lifecycle: config.ExperimentLifecycle{Cluster: config.ClusterLifecycleReuse},
		Tools: config.ToolConfig{
			SchedulerPlugins: config.SchedulerPluginsConfig{Enabled: true},
			ChaosInjector:    config.ChaosInjectorConfig{Enabled: true},
		},
	}

	got := Build(experiment).Runs[0].Phases
	want := []string{
		"provision-cluster",
		"install-scheduler",
		"prepare-application-namespace",
		"deploy-application",
		"deploy-chaos",
		"run-load-generator",
		"collect-artifacts",
		"cleanup",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("phases = %v, want %v", got, want)
	}
}

func TestReuseLifecycleResetsAfterFirstRun(t *testing.T) {
	experiment := config.Experiment{
		Lifecycle: config.ExperimentLifecycle{Cluster: config.ClusterLifecycleReuse},
	}
	first := phasesFor(experiment, 1)
	second := phasesFor(experiment, 2)
	if first[0] != "provision-cluster" {
		t.Fatalf("first phases = %v", first)
	}
	if second[0] != "reset-experiment-resources" {
		t.Fatalf("second phases = %v", second)
	}
}
