package executor

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/unict-cclab/experiment-executor/internal/config"
)

type aggregateMetric struct {
	Count              int     `json:"count"`
	Mean               float64 `json:"mean"`
	SampleStdDeviation float64 `json:"sampleStdDeviation"`
	Min                float64 `json:"min"`
	Max                float64 `json:"max"`
}

type experimentSummary struct {
	Experiment     string                     `json:"experiment"`
	SuccessfulRuns int                        `json:"successfulRuns"`
	Metrics        map[string]aggregateMetric `json:"metrics"`
}

func aggregateRunSummaries(experiment *config.Experiment) error {
	values := make(map[string][]float64)
	successful := 0
	for runNumber := 1; runNumber <= experiment.Runs; runNumber++ {
		path := filepath.Join(
			experiment.RunsDir(), formatRunID(runNumber), "load-gen", "summary.json",
		)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			return err
		}
		flattenNumeric("", document, values)
		successful++
	}
	metrics := make(map[string]aggregateMetric, len(values))
	for name, samples := range values {
		metrics[name] = summarize(samples)
	}
	summary := experimentSummary{Experiment: experiment.Name, SuccessfulRuns: successful, Metrics: metrics}
	return writeJSON(filepath.Join(experiment.SourceDir, "summary.json"), summary)
}

func flattenNumeric(prefix string, value any, result map[string][]float64) {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			name := key
			if prefix != "" {
				name = prefix + "." + key
			}
			flattenNumeric(name, typed[key], result)
		}
	case float64:
		result[prefix] = append(result[prefix], typed)
	}
}

func summarize(values []float64) aggregateMetric {
	result := aggregateMetric{Count: len(values), Min: values[0], Max: values[0]}
	for _, value := range values {
		result.Mean += value
		result.Min = math.Min(result.Min, value)
		result.Max = math.Max(result.Max, value)
	}
	result.Mean /= float64(len(values))
	if len(values) > 1 {
		for _, value := range values {
			delta := value - result.Mean
			result.SampleStdDeviation += delta * delta
		}
		result.SampleStdDeviation = math.Sqrt(result.SampleStdDeviation / float64(len(values)-1))
	}
	return result
}

func formatRunID(runNumber int) string {
	return "run-" + leftPad(runNumber, 3)
}

func leftPad(value, width int) string {
	text := ""
	for value > 0 {
		text = string(rune('0'+value%10)) + text
		value /= 10
	}
	if text == "" {
		text = "0"
	}
	for len(text) < width {
		text = "0" + text
	}
	return text
}
