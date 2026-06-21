package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/unict-cclab/experiment-executor/internal/config"
	"github.com/unict-cclab/experiment-executor/internal/plan"
	"gopkg.in/yaml.v3"
)

type Options struct {
	DryRun bool
}

type Runner struct {
	experiment *config.Experiment
	options    Options
}

type runState struct {
	Experiment  string     `json:"experiment"`
	Run         string     `json:"run"`
	Number      int        `json:"number"`
	Status      string     `json:"status"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type runFiles struct {
	root              string
	configs           string
	logs              string
	proxmoxConfig     string
	kubeconfig        string
	namespaceManifest string
	schedulerValues   string
	deschedulerPolicy string
	application       string
	chaosManifest     string
	loadGenConfig     string
}

type nodeInfo struct {
	Name       string
	InternalIP string
	Worker     bool
}

func Run(ctx context.Context, experiment *config.Experiment, options Options) error {
	runner := &Runner{experiment: experiment, options: options}
	if err := os.MkdirAll(experiment.ConfigDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(experiment.RunsDir(), 0o755); err != nil {
		return err
	}
	if err := writeYAML(filepath.Join(experiment.ConfigDir(), "experiment.resolved.yaml"), experiment); err != nil {
		return err
	}
	if !options.DryRun {
		if err := runner.preflight(); err != nil {
			return err
		}
	}
	executionPlan := plan.Build(experiment)
	for _, plannedRun := range executionPlan.Runs {
		if err := runner.runOne(ctx, *experiment, plannedRun); err != nil {
			return err
		}
	}
	return aggregateRunSummaries(experiment)
}

func (r *Runner) runOne(ctx context.Context, experiment config.Experiment, planned plan.Run) (runErr error) {
	files, err := r.prepareFiles(experiment, planned)
	if err != nil {
		return err
	}
	if experiment.Lifecycle.KubeconfigPath != "" {
		src := r.experiment.ResolvePath(experiment.Lifecycle.KubeconfigPath)
		if err := copyFile(src, files.kubeconfig); err != nil {
			return fmt.Errorf("copying kubeconfig from %s: %w", src, err)
		}
	}
	state := runState{
		Experiment: planned.Experiment,
		Run:        planned.ID,
		Number:     planned.Number,
		Status:     "running",
		StartedAt:  time.Now().UTC(),
	}
	defer func() {
		now := time.Now().UTC()
		state.CompletedAt = &now
		if runErr != nil {
			state.Status = "failed"
			state.Error = runErr.Error()
		} else if r.options.DryRun {
			state.Status = "dry-run"
		} else {
			state.Status = "complete"
		}
		_ = writeJSON(filepath.Join(files.root, "run.json"), state)
	}()
	if err := writeJSON(filepath.Join(files.root, "run.json"), state); err != nil {
		return err
	}

	fmt.Printf("\n==> %s / %s\n", planned.Experiment, planned.ID)
	if err := r.prepareProxmoxConfig(experiment, &files); err != nil {
		return err
	}

	chaosDeployed := false
	deschedulerInstalled := false
	defer func() {
		if deschedulerInstalled {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_ = r.uninstallDescheduler(cleanupCtx, experiment, files, "descheduler-cleanup")
		}
		if chaosDeployed {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_ = r.deleteChaos(cleanupCtx, files)
		}
	}()
	for _, phase := range planned.Phases {
		fmt.Printf("--> %s\n", phase)
		switch phase {
		case "delete-cluster":
			if err := r.proxmox(ctx, files, "cluster", "delete", "-c", files.proxmoxConfig); err != nil {
				return err
			}
		case "provision-cluster":
			if err := r.proxmox(ctx, files, "cluster", "create", "-c", files.proxmoxConfig); err != nil {
				return err
			}
		case "validate-cluster-access":
			if err := r.command(ctx, files, "cluster-access", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "cluster-info"); err != nil {
				return err
			}
		case "reset-experiment-resources":
			if err := r.resetResources(ctx, experiment, files); err != nil {
				return err
			}
		case "configure-observability":
			if err := r.configureObservability(ctx, experiment, files); err != nil {
				return err
			}
		case "install-scheduler":
			if err := r.installScheduler(ctx, experiment, files); err != nil {
				return err
			}
		case "install-descheduler":
			if err := r.installDescheduler(ctx, experiment, files); err != nil {
				return err
			}
			deschedulerInstalled = true
		case "prepare-application-namespace":
			if err := r.prepareNamespace(ctx, experiment, files); err != nil {
				return err
			}
		case "deploy-application":
			nodes, err := r.nodes(ctx, experiment, files)
			if err != nil {
				return err
			}
			if err := r.renderApplication(experiment, files, nodes); err != nil {
				return err
			}
			if err := r.command(ctx, files, "application-apply", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "apply", "-n", experiment.Tools.Application.Namespace, "-f", files.application); err != nil {
				return err
			}
			if err := r.command(ctx, files, "application-wait", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "wait", "-n", experiment.Tools.Application.Namespace, "--for=condition=Available", "deployment", "-l", "group="+experiment.Tools.Application.Group, "--timeout=10m"); err != nil {
				return err
			}
		case "deploy-chaos":
			if err := r.deployChaos(ctx, experiment, files); err != nil {
				return err
			}
			chaosDeployed = true
		case "run-load-generator":
			if err := r.prepareLoadGen(ctx, experiment, files); err != nil {
				return err
			}
			if err := r.command(ctx, files, "load-gen", nil, r.loadGen(), "run", "-c", files.loadGenConfig); err != nil {
				return err
			}
		case "collect-artifacts":
			// Tool outputs already live below this run directory.
		case "cleanup":
			if experiment.Tools.ChaosInjector.Enabled {
				if err := r.deleteChaos(ctx, files); err != nil {
					return err
				}
				chaosDeployed = false
			}
			if experiment.Tools.Descheduler.Enabled {
				if err := r.uninstallDescheduler(ctx, experiment, files, "descheduler-cleanup"); err != nil {
					return err
				}
				deschedulerInstalled = false
			}
			if experiment.Tools.SchedulerPlugins.Enabled {
				if err := r.command(ctx, files, "scheduler-cleanup", nil, r.helm(), "uninstall", experiment.Tools.SchedulerPlugins.Release, "--namespace", experiment.Tools.SchedulerPlugins.Namespace, "--kubeconfig", files.kubeconfig, "--ignore-not-found"); err != nil {
					return err
				}
			}
			selector := "group=" + experiment.Tools.Application.Group
			if err := r.command(ctx, files, "application-cleanup", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "delete", "all,configmap", "-n", experiment.Tools.Application.Namespace, "-l", selector, "--ignore-not-found"); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported phase %q", phase)
		}
	}
	return nil
}

func (r *Runner) prepareFiles(experiment config.Experiment, planned plan.Run) (runFiles, error) {
	configDir := r.experiment.ConfigDir()
	files := runFiles{
		root:              planned.OutputDir,
		configs:           configDir,
		logs:              filepath.Join(planned.OutputDir, "logs"),
		proxmoxConfig:     filepath.Join(configDir, "proxmox-k3s.yaml"),
		kubeconfig:        filepath.Join(configDir, "kubeconfig"),
		namespaceManifest: filepath.Join(configDir, "namespace.yaml"),
		schedulerValues:   filepath.Join(configDir, "scheduler-values.yaml"),
		deschedulerPolicy: filepath.Join(configDir, "descheduler-values.yaml"),
		application:       filepath.Join(configDir, "application.yaml"),
		chaosManifest:     filepath.Join(configDir, "network-chaos.yaml"),
		loadGenConfig:     filepath.Join(configDir, "load-gen.yaml"),
	}
	for _, dir := range []string{files.root, files.configs, files.logs, filepath.Join(files.root, "load-gen")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return runFiles{}, fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	return files, nil
}

func (r *Runner) prepareProxmoxConfig(experiment config.Experiment, files *runFiles) error {
	value, err := cloneMap(experiment.Tools.ProxmoxK3s.Config)
	if err != nil {
		return err
	}
	clusters, ok := value["clusters"].([]any)
	if !ok || len(clusters) != 1 {
		return errors.New("proxmoxK3s.config.clusters must contain exactly one cluster")
	}
	for _, raw := range clusters {
		cluster, ok := raw.(map[string]any)
		if !ok {
			return errors.New("invalid proxmox cluster configuration")
		}
		sourceKubeconfig, _ := cluster["kubeconfig_path"].(string)
		if sourceKubeconfig != "" {
			sourceKubeconfig = r.experiment.ResolvePath(sourceKubeconfig)
			if sourceKubeconfig != files.kubeconfig {
				if _, err := os.Stat(sourceKubeconfig); err == nil {
					if err := copyFile(sourceKubeconfig, files.kubeconfig); err != nil {
						return fmt.Errorf("copying kubeconfig into experiment config: %w", err)
					}
				}
			}
		}
		cluster["kubeconfig_path"] = files.kubeconfig
		addons := ensureMap(cluster, "addons")
		monAgent := ensureMap(addons, "mon_agent")
		monAgent["enabled"] = experiment.Tools.MonAgent.Enabled
		if experiment.Tools.MonAgent.Enabled {
			monAgent["version"] = experiment.Tools.MonAgent.Version
			monAgent["scrape_period_seconds"] = durationSeconds(experiment.Tools.MonAgent.ScrapeInterval)
			monAgent["promql_range"] = experiment.Tools.MonAgent.MetricsRange
		}
		mentat := ensureMap(addons, "mentat")
		mentat["enabled"] = experiment.Tools.Mentat.Enabled
		if experiment.Tools.Mentat.Enabled {
			mentat["version"] = experiment.Tools.Mentat.Version
			mentat["sleep_seconds"] = durationSeconds(experiment.Tools.Mentat.ProbeInterval)
		}
	}
	return writeYAML(files.proxmoxConfig, value)
}

func (r *Runner) proxmox(ctx context.Context, files runFiles, args ...string) error {
	stdin := io.Reader(nil)
	if len(args) >= 2 && args[0] == "cluster" && args[1] == "delete" {
		stdin = strings.NewReader("y\n")
	}
	return r.command(ctx, files, "proxmox-"+strings.Join(args[:2], "-"), stdin, r.proxmoxK3s(), args...)
}

func (r *Runner) configureObservability(ctx context.Context, experiment config.Experiment, files runFiles) error {
	if r.options.DryRun {
		return r.command(ctx, files, "observability", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "get", "namespace", "observability")
	}
	if experiment.Tools.MonAgent.Enabled {
		image := "ghcr.io/unict-cclab/mon-agent:" + experiment.Tools.MonAgent.Version
		if err := r.command(ctx, files, "mon-agent-image", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "-n", "observability", "set", "image", "deployment/mon-agent", "mon-agent="+image); err != nil {
			return err
		}
		if err := r.command(ctx, files, "mon-agent-config", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "-n", "observability", "set", "env", "deployment/mon-agent", "SCRAPE_PERIOD_SECONDS="+strconv.Itoa(durationSeconds(experiment.Tools.MonAgent.ScrapeInterval)), "PROMQL_RANGE="+experiment.Tools.MonAgent.MetricsRange); err != nil {
			return err
		}
	}
	if experiment.Tools.Mentat.Enabled {
		image := "ghcr.io/unict-cclab/mentat:" + experiment.Tools.Mentat.Version
		if err := r.command(ctx, files, "mentat-image", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "-n", "observability", "set", "image", "daemonset/mentat", "mentat="+image); err != nil {
			return err
		}
		if err := r.command(ctx, files, "mentat-config", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "-n", "observability", "set", "env", "daemonset/mentat", "SLEEP_SECONDS="+strconv.Itoa(durationSeconds(experiment.Tools.Mentat.ProbeInterval))); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) installScheduler(ctx context.Context, experiment config.Experiment, files runFiles) error {
	if err := writeYAML(files.schedulerValues, experiment.Tools.SchedulerPlugins.Values); err != nil {
		return err
	}
	chart := r.experiment.ResolvePath(experiment.Tools.SchedulerPlugins.Chart)
	return r.command(ctx, files, "scheduler-install", nil, r.helm(), "upgrade", "--install", experiment.Tools.SchedulerPlugins.Release, chart, "--namespace", experiment.Tools.SchedulerPlugins.Namespace, "--create-namespace", "--values", files.schedulerValues, "--kubeconfig", files.kubeconfig, "--wait", "--timeout", "10m")
}

func (r *Runner) resetResources(ctx context.Context, experiment config.Experiment, files runFiles) error {
	if experiment.Tools.ChaosInjector.Enabled {
		if err := r.command(ctx, files, "chaos-reset", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "delete", "networkchaos", "-n", experiment.Tools.Application.Namespace, "-l", "app.kubernetes.io/managed-by=node-latency-chaos-injector", "--ignore-not-found"); err != nil {
			return err
		}
	}
	if experiment.Tools.Descheduler.Enabled {
		if err := r.uninstallDescheduler(ctx, experiment, files, "descheduler-reset"); err != nil {
			return err
		}
	}
	if experiment.Tools.SchedulerPlugins.Enabled {
		if err := r.command(ctx, files, "scheduler-reset", nil, r.helm(), "uninstall", experiment.Tools.SchedulerPlugins.Release, "--namespace", experiment.Tools.SchedulerPlugins.Namespace, "--kubeconfig", files.kubeconfig, "--ignore-not-found"); err != nil {
			return err
		}
	}
	selector := "group=" + experiment.Tools.Application.Group
	return r.command(ctx, files, "application-reset", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "delete", "all,configmap", "-n", experiment.Tools.Application.Namespace, "-l", selector, "--ignore-not-found")
}

func (r *Runner) deployChaos(ctx context.Context, experiment config.Experiment, files runFiles) error {
	chaos := experiment.Tools.ChaosInjector
	env := []string{
		"KUBECONFIG=" + files.kubeconfig,
		"KUBECTL=" + r.kubectl(),
		"WORKLOAD_NAMESPACE=" + experiment.Tools.Application.Namespace,
		"NODE_GROUP_LABEL=" + chaos.NodeGroupLabel,
		"CROSS_ZONE_LATENCY=" + chaos.CrossZoneLatency,
		"CROSS_ZONE_BANDWIDTH=" + chaos.CrossZoneBandwidth,
		"BANDWIDTH_LIMIT=" + strconv.Itoa(chaos.BandwidthLimit),
		"BANDWIDTH_BUFFER=" + strconv.Itoa(chaos.BandwidthBuffer),
		"JITTER=" + chaos.Jitter,
		"CORRELATION=" + chaos.Correlation,
		"NODE_SELECTOR=" + chaos.NodeSelector,
	}
	return r.commandEnv(ctx, files, "chaos-deploy", nil, env, r.chaosInjector(), "deploy", files.chaosManifest)
}

func (r *Runner) deleteChaos(ctx context.Context, files runFiles) error {
	return r.commandEnv(ctx, files, "chaos-delete", nil, []string{"KUBECONFIG=" + files.kubeconfig, "KUBECTL=" + r.kubectl()}, r.chaosInjector(), "delete", files.chaosManifest)
}

func (r *Runner) prepareNamespace(ctx context.Context, experiment config.Experiment, files runFiles) error {
	labels := experiment.Tools.ApplicationNamespaceLabels()
	manifest := map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata": map[string]any{
			"name":   experiment.Tools.Application.Namespace,
			"labels": labels,
		},
	}
	if err := writeYAML(files.namespaceManifest, manifest); err != nil {
		return err
	}
	return r.command(ctx, files, "namespace", nil, r.kubectl(), "--kubeconfig", files.kubeconfig, "apply", "-f", files.namespaceManifest)
}

func (r *Runner) nodes(ctx context.Context, experiment config.Experiment, files runFiles) ([]nodeInfo, error) {
	if r.options.DryRun {
		return declaredNodes(experiment.Tools.ProxmoxK3s.Config), nil
	}
	output, err := exec.CommandContext(ctx, r.kubectl(), "--kubeconfig", files.kubeconfig, "get", "nodes", "-o", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("discovering nodes: %w", err)
	}
	var document struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Addresses []struct {
					Type    string `json:"type"`
					Address string `json:"address"`
				} `json:"addresses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(output, &document); err != nil {
		return nil, fmt.Errorf("decoding nodes: %w", err)
	}
	var nodes []nodeInfo
	for _, item := range document.Items {
		node := nodeInfo{Name: item.Metadata.Name, Worker: true}
		if _, ok := item.Metadata.Labels["node-role.kubernetes.io/control-plane"]; ok {
			node.Worker = false
		}
		for _, address := range item.Status.Addresses {
			if address.Type == "InternalIP" {
				node.InternalIP = address.Address
				break
			}
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (r *Runner) renderApplication(experiment config.Experiment, files runFiles, nodes []nodeInfo) error {
	selected := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if experiment.Tools.Application.ProxyNodes == "none" {
			continue
		}
		if experiment.Tools.Application.ProxyNodes == "workers" && !node.Worker {
			continue
		}
		selected = append(selected, node.Name)
	}
	sort.Strings(selected)
	templatePath := r.experiment.ResolvePath(experiment.Tools.Application.Template)
	tmpl, err := template.New(filepath.Base(templatePath)).Option("missingkey=error").ParseFiles(templatePath)
	if err != nil {
		return fmt.Errorf("parsing application template: %w", err)
	}
	file, err := os.Create(files.application)
	if err != nil {
		return err
	}
	defer file.Close()
	values := map[string]any{
		"schedulerName": experiment.Tools.Application.SchedulerName,
		"group":         experiment.Tools.Application.Group,
		"proxyNodes":    selected,
		"minReplicas":   experiment.Tools.Application.MinReplicas,
		"proxyNodePort": experiment.Tools.Application.ProxyNodePort,
	}
	if err := tmpl.ExecuteTemplate(file, filepath.Base(templatePath), values); err != nil {
		return fmt.Errorf("rendering application template: %w", err)
	}
	return nil
}

func (r *Runner) installDescheduler(ctx context.Context, experiment config.Experiment, files runFiles) error {
	tool := experiment.Tools.Descheduler
	values, err := cloneMap(tool.Values)
	if err != nil {
		return err
	}
	values["kind"] = "Deployment"
	values["deschedulingInterval"] = tool.Interval
	policy, err := cloneMap(tool.Policy)
	if err != nil {
		return err
	}
	if apiVersion, ok := policy["apiVersion"].(string); ok && apiVersion != "" {
		values["deschedulerPolicyAPIVersion"] = apiVersion
	}
	delete(policy, "apiVersion")
	delete(policy, "kind")
	values["deschedulerPolicy"] = policy
	cmdOptions := ensureMap(values, "cmdOptions")
	cmdOptions["v"] = 3
	if tool.DryRun {
		cmdOptions["dry-run"] = true
	}
	if err := writeYAML(files.deschedulerPolicy, values); err != nil {
		return err
	}
	chart := r.experiment.ResolvePath(tool.Chart)
	return r.command(ctx, files, "descheduler-install", nil, r.helm(), "upgrade", "--install", tool.Release, chart, "--namespace", tool.Namespace, "--create-namespace", "--values", files.deschedulerPolicy, "--kubeconfig", files.kubeconfig, "--wait", "--timeout", "10m")
}

func (r *Runner) uninstallDescheduler(ctx context.Context, experiment config.Experiment, files runFiles, logName string) error {
	tool := experiment.Tools.Descheduler
	return r.command(ctx, files, logName, nil, r.helm(), "uninstall", tool.Release, "--namespace", tool.Namespace, "--kubeconfig", files.kubeconfig, "--ignore-not-found")
}

func (r *Runner) prepareLoadGen(ctx context.Context, experiment config.Experiment, files runFiles) error {
	value, err := cloneMap(experiment.Tools.LoadGen.Config)
	if err != nil {
		return err
	}
	locustfile, _ := value["locustfile"].(string)
	if locustfile == "" {
		return errors.New("loadGen.config.locustfile is required")
	}
	value["locustfile"] = r.experiment.ResolvePath(locustfile)
	value["name"] = "load-gen"
	value["output_dir"] = filepath.Join(files.root, "load-gen")
	kubernetes := ensureMap(value, "kubernetes")
	kubernetes["enabled"] = true
	kubernetes["namespace"] = experiment.Tools.Application.Namespace
	kubernetes["selector"] = "group=" + experiment.Tools.Application.Group
	kubernetes["kubeconfig"] = files.kubeconfig

	nodes, err := r.nodes(ctx, experiment, files)
	if err != nil {
		return err
	}
	nodePort := 0
	if !r.options.DryRun {
		output, err := exec.CommandContext(ctx, r.kubectl(), "--kubeconfig", files.kubeconfig, "get", "service", "node-proxy", "-n", experiment.Tools.Application.Namespace, "-o", "json").Output()
		if err != nil {
			return fmt.Errorf("discovering node-proxy service: %w", err)
		}
		var service struct {
			Spec struct {
				Ports []struct {
					NodePort int `json:"nodePort"`
				} `json:"ports"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(output, &service); err != nil || len(service.Spec.Ports) == 0 {
			return errors.New("node-proxy service has no NodePort")
		}
		nodePort = service.Spec.Ports[0].NodePort
	}
	var endpoints []any
	for _, node := range nodes {
		if node.InternalIP == "" {
			continue
		}
		if experiment.Tools.Application.ProxyNodes == "workers" && !node.Worker {
			continue
		}
		endpoints = append(endpoints, map[string]any{"url": fmt.Sprintf("http://%s:%d", node.InternalIP, nodePort)})
	}
	if len(endpoints) == 0 {
		return errors.New("no node-proxy endpoints discovered")
	}
	value["endpoints"] = endpoints
	delete(value, "host")
	return writeYAML(files.loadGenConfig, value)
}

func (r *Runner) command(ctx context.Context, files runFiles, logName string, stdin io.Reader, name string, args ...string) error {
	return r.commandEnv(ctx, files, logName, stdin, nil, name, args...)
}

func (r *Runner) commandEnv(ctx context.Context, files runFiles, logName string, stdin io.Reader, environment []string, name string, args ...string) error {
	line := strings.Join(append([]string{name}, args...), " ")
	if r.options.DryRun {
		fmt.Printf("    [dry-run] %s\n", line)
		return os.WriteFile(filepath.Join(files.logs, logName+".log"), []byte("[dry-run] "+line+"\n"), 0o644)
	}
	log, err := os.Create(filepath.Join(files.logs, logName+".log"))
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), environment...)
	cmd.Stdin = stdin
	cmd.Stdout = io.MultiWriter(os.Stdout, log)
	cmd.Stderr = io.MultiWriter(os.Stderr, log)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command %q failed: %w", line, err)
	}
	return nil
}

func (r *Runner) preflight() error {
	required := map[string]string{
		"kubectl":  r.kubectl(),
		"Load Gen": r.loadGen(),
	}
	if r.experiment.Lifecycle.Cluster != config.ClusterLifecycleExisting {
		required["proxmox-k3s"] = r.proxmoxK3s()
	}
	if r.experiment.Tools.SchedulerPlugins.Enabled || r.experiment.Tools.Descheduler.Enabled {
		required["Helm"] = r.helm()
	}
	if r.experiment.Tools.ChaosInjector.Enabled {
		required["chaos-injector"] = r.chaosInjector()
	}
	for label, command := range required {
		if strings.ContainsRune(command, filepath.Separator) {
			info, err := os.Stat(command)
			if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
				return fmt.Errorf("%s executable is unavailable: %s", label, command)
			}
			continue
		}
		if _, err := exec.LookPath(command); err != nil {
			return fmt.Errorf("%s executable %q was not found in PATH", label, command)
		}
	}
	return nil
}

func (r *Runner) proxmoxK3s() string {
	return r.experiment.ResolveCommand(r.experiment.Commands.ProxmoxK3s)
}
func (r *Runner) kubectl() string { return r.experiment.ResolveCommand(r.experiment.Commands.Kubectl) }
func (r *Runner) helm() string    { return r.experiment.ResolveCommand(r.experiment.Commands.Helm) }
func (r *Runner) loadGen() string { return r.experiment.ResolveCommand(r.experiment.Commands.LoadGen) }
func (r *Runner) chaosInjector() string {
	return r.experiment.ResolveCommand(r.experiment.Commands.ChaosInjector)
}

func durationSeconds(value string) int {
	duration, _ := time.ParseDuration(value)
	return int(duration.Seconds())
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if value, ok := parent[key].(map[string]any); ok {
		return value
	}
	value := make(map[string]any)
	parent[key] = value
	return value
}

func cloneMap(value map[string]any) (map[string]any, error) {
	if value == nil {
		return make(map[string]any), nil
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := yaml.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func declaredNodes(proxmoxConfig map[string]any) []nodeInfo {
	clusters, _ := proxmoxConfig["clusters"].([]any)
	if len(clusters) == 0 {
		return nil
	}
	cluster, _ := clusters[0].(map[string]any)
	var result []nodeInfo
	for _, section := range []struct {
		key    string
		worker bool
	}{{"control_plane", false}, {"workers", true}} {
		items, _ := cluster[section.key].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			name, _ := item["name"].(string)
			ip, _ := item["ip"].(string)
			result = append(result, nodeInfo{Name: name, InternalIP: ip, Worker: section.worker})
		}
	}
	return result
}

func writeYAML(path string, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o600)
}
