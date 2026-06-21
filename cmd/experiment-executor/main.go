package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/unict-cclab/experiment-executor/internal/config"
	"github.com/unict-cclab/experiment-executor/internal/executor"
	"github.com/unict-cclab/experiment-executor/internal/plan"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "validate":
		err = validateCommand(os.Args[2:])
	case "plan":
		err = planCommand(os.Args[2:])
	case "run":
		err = runCommand(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runCommand(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := flags.String("config", "experiment.yaml", "experiment configuration file")
	flags.StringVar(configPath, "c", "experiment.yaml", "experiment configuration file")
	dryRun := flags.Bool("dry-run", false, "render and print commands without changing external state")
	if err := flags.Parse(args); err != nil {
		return err
	}
	experiment, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return executor.Run(ctx, experiment, executor.Options{DryRun: *dryRun})
}

func validateCommand(args []string) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	configPath := flags.String("config", "experiment.yaml", "experiment configuration file")
	flags.StringVar(configPath, "c", "experiment.yaml", "experiment configuration file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	experiment, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	runLabel := "runs"
	if experiment.Runs == 1 {
		runLabel = "run"
	}
	fmt.Printf("experiment %q is valid (%d %s)\n", experiment.Name, experiment.Runs, runLabel)
	return nil
}

func planCommand(args []string) error {
	flags := flag.NewFlagSet("plan", flag.ContinueOnError)
	configPath := flags.String("config", "experiment.yaml", "experiment configuration file")
	flags.StringVar(configPath, "c", "experiment.yaml", "experiment configuration file")
	jsonOutput := flags.Bool("json", false, "write the execution plan as JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	experiment, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	executionPlan := plan.Build(experiment)
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(executionPlan)
	}
	plan.WriteText(os.Stdout, executionPlan)
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `Experiment Executor

Usage:
  experiment-executor validate -c <experiment.yaml>
  experiment-executor plan -c <experiment.yaml> [--json]
  experiment-executor run -c <experiment.yaml> [--dry-run]

Commands:
  validate  Parse and validate an experiment without changing external state
  plan      Show its runs, phases, and artifact paths
  run       Execute experiment runs sequentially and collect artifacts`)
}
