package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/unict-cclab/experiment-executor/internal/config"
)

func RunSuite(ctx context.Context, suite *config.Suite, entries []config.LoadedSuiteEntry, options Options) error {
	for index, entry := range entries {
		fmt.Printf("\n==== Suite %s: experiment %d/%d (%s) ====\n", suite.Name, index+1, len(entries), entry.Name)
		if err := Run(ctx, entry.Experiment, options); err != nil {
			return fmt.Errorf("experiment %q failed: %w", entry.Name, err)
		}
	}
	if options.DryRun {
		return nil
	}
	if err := os.MkdirAll(suite.ResolvedOutputDir(), 0o755); err != nil {
		return err
	}
	args := []string{"compare", "--output-dir", suite.ResolvedOutputDir()}
	for _, entry := range entries {
		args = append(args, "--experiment", entry.Name+"="+entry.Experiment.SourceDir)
	}
	command := entries[0].Experiment.ResolveCommand(entries[0].Experiment.Commands.LoadGen)
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("generating suite comparison: %w", err)
	}
	return nil
}
