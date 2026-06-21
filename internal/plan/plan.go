package plan

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/unict-cclab/experiment-executor/internal/config"
)

type ExecutionPlan struct {
	Experiment string `json:"experiment"`
	ConfigDir  string `json:"configDir"`
	Runs       []Run  `json:"runs"`
}

type Run struct {
	Experiment  string          `json:"experiment"`
	Number      int             `json:"number"`
	ID          string          `json:"id"`
	OutputDir   string          `json:"outputDir"`
	Lifecycle   string          `json:"clusterLifecycle"`
	Application ApplicationPlan `json:"application"`
	Phases      []string        `json:"phases"`
}

type ApplicationPlan struct {
	Name            string            `json:"name"`
	Namespace       string            `json:"namespace"`
	Group           string            `json:"group"`
	NamespaceLabels map[string]string `json:"namespaceLabels"`
}

func Build(experiment *config.Experiment) ExecutionPlan {
	result := ExecutionPlan{
		Experiment: experiment.Name,
		ConfigDir:  experiment.ConfigDir(),
	}
	clusterLifecycle := experiment.Lifecycle.Cluster
	if clusterLifecycle == "" {
		clusterLifecycle = config.ClusterLifecycleExisting
	}
	application := ApplicationPlan{
		Name:            experiment.Tools.Application.Name,
		Namespace:       experiment.Tools.Application.Namespace,
		Group:           experiment.Tools.Application.Group,
		NamespaceLabels: experiment.Tools.ApplicationNamespaceLabels(),
	}
	for runNumber := 1; runNumber <= experiment.Runs; runNumber++ {
		runPhases := phasesFor(*experiment, runNumber)
		id := fmt.Sprintf("run-%03d", runNumber)
		result.Runs = append(result.Runs, Run{
			Experiment:  experiment.Name,
			Number:      runNumber,
			ID:          id,
			OutputDir:   filepath.Join(experiment.RunsDir(), id),
			Lifecycle:   clusterLifecycle,
			Application: application,
			Phases:      append([]string(nil), runPhases...),
		})
	}
	return result
}

func phasesFor(experiment config.Experiment, runNumber int) []string {
	var result []string
	switch experiment.Lifecycle.Cluster {
	case config.ClusterLifecycleRecreate:
		result = append(result, "delete-cluster", "provision-cluster")
	case config.ClusterLifecycleReuse:
		if runNumber == 1 {
			result = append(result, "provision-cluster")
		} else {
			result = append(result, "reset-experiment-resources")
		}
	case "", config.ClusterLifecycleExisting:
		result = append(result, "validate-cluster-access", "reset-experiment-resources")
	}
	if experiment.Tools.MonAgent.Enabled || experiment.Tools.Mentat.Enabled {
		result = append(result, "configure-observability")
	}
	if experiment.Tools.SchedulerPlugins.Enabled {
		result = append(result, "install-scheduler")
	}
	if experiment.Tools.Descheduler.Enabled {
		result = append(result, "install-descheduler")
	}
	result = append(result,
		"prepare-application-namespace",
		"deploy-application",
	)
	result = append(result, phasesForChaos(experiment)...)
	return append(result,
		"run-load-generator",
		"collect-artifacts",
		"cleanup",
	)
}

func phasesForChaos(experiment config.Experiment) []string {
	if experiment.Tools.ChaosInjector.Enabled {
		return []string{"deploy-chaos"}
	}
	return nil
}

func WriteText(out io.Writer, executionPlan ExecutionPlan) {
	fmt.Fprintf(out, "Experiment: %s\n", executionPlan.Experiment)
	fmt.Fprintf(out, "Config:     %s\n", executionPlan.ConfigDir)
	fmt.Fprintf(out, "Runs:     %d\n", len(executionPlan.Runs))
	for _, run := range executionPlan.Runs {
		fmt.Fprintf(out, "\n%s / %s (run %d)\n", run.Experiment, run.ID, run.Number)
		fmt.Fprintf(out, "  artifacts: %s\n", run.OutputDir)
		fmt.Fprintf(out, "  cluster lifecycle: %s\n", run.Lifecycle)
		fmt.Fprintf(out, "  application: %s (namespace %s, group %s)\n", run.Application.Name, run.Application.Namespace, run.Application.Group)
		labelKeys := make([]string, 0, len(run.Application.NamespaceLabels))
		for key := range run.Application.NamespaceLabels {
			labelKeys = append(labelKeys, key)
		}
		sort.Strings(labelKeys)
		for _, key := range labelKeys {
			fmt.Fprintf(out, "    label: %s=%s\n", key, run.Application.NamespaceLabels[key])
		}
		for _, phase := range run.Phases {
			fmt.Fprintf(out, "  - %s\n", phase)
		}
	}
}
