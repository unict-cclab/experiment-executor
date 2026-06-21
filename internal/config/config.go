package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var validName = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

const (
	ClusterLifecycleRecreate = "recreate"
	ClusterLifecycleReuse    = "reuse"
	ClusterLifecycleExisting = "existing"
)

type Commands struct {
	ProxmoxK3s    string `yaml:"proxmoxK3s" json:"proxmoxK3s"`
	Kubectl       string `yaml:"kubectl" json:"kubectl"`
	Helm          string `yaml:"helm" json:"helm"`
	ChaosInjector string `yaml:"chaosInjector" json:"chaosInjector"`
	LoadGen       string `yaml:"loadGen" json:"loadGen"`
}

type Experiment struct {
	Name      string              `yaml:"name" json:"name"`
	Runs      int                 `yaml:"runs" json:"runs"`
	Commands  Commands            `yaml:"commands" json:"commands"`
	Lifecycle ExperimentLifecycle `yaml:"lifecycle" json:"lifecycle"`
	Tools     ToolConfig          `yaml:"tools" json:"tools"`
	SourceDir string              `yaml:"-" json:"-"`
}

type ExperimentLifecycle struct {
	Cluster string `yaml:"cluster" json:"cluster"`
}

type ToolConfig struct {
	ProxmoxK3s       ProxmoxK3sConfig       `yaml:"proxmoxK3s" json:"proxmoxK3s"`
	MonAgent         MonAgentConfig         `yaml:"monAgent" json:"monAgent"`
	Mentat           MentatConfig           `yaml:"mentat" json:"mentat"`
	SchedulerPlugins SchedulerPluginsConfig `yaml:"schedulerPlugins" json:"schedulerPlugins"`
	Descheduler      DeschedulerConfig      `yaml:"descheduler" json:"descheduler"`
	ChaosInjector    ChaosInjectorConfig    `yaml:"chaosInjector" json:"chaosInjector"`
	Application      ApplicationConfig      `yaml:"application" json:"application"`
	LoadGen          LoadGenConfig          `yaml:"loadGen" json:"loadGen"`
}

type ProxmoxK3sConfig struct {
	Config map[string]any `yaml:"config" json:"config"`
}

type MonAgentConfig struct {
	Enabled        bool   `yaml:"enabled" json:"enabled"`
	Version        string `yaml:"version" json:"version"`
	ScrapeInterval string `yaml:"scrapeInterval" json:"scrapeInterval"`
	MetricsRange   string `yaml:"metricsRange" json:"metricsRange"`
}

type MentatConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`
	Version       string `yaml:"version" json:"version"`
	ProbeInterval string `yaml:"probeInterval" json:"probeInterval"`
}

type SchedulerPluginsConfig struct {
	Enabled   bool           `yaml:"enabled" json:"enabled"`
	Chart     string         `yaml:"chart" json:"chart"`
	Release   string         `yaml:"release" json:"release"`
	Namespace string         `yaml:"namespace" json:"namespace"`
	Values    map[string]any `yaml:"values" json:"values"`
}

type DeschedulerConfig struct {
	Enabled   bool           `yaml:"enabled" json:"enabled"`
	Chart     string         `yaml:"chart" json:"chart"`
	Release   string         `yaml:"release" json:"release"`
	Namespace string         `yaml:"namespace" json:"namespace"`
	Policy    map[string]any `yaml:"policy" json:"policy"`
	Interval  string         `yaml:"interval,omitempty" json:"interval,omitempty"`
	DryRun    bool           `yaml:"dryRun" json:"dryRun"`
	Values    map[string]any `yaml:"values,omitempty" json:"values,omitempty"`
}

type ChaosInjectorConfig struct {
	Enabled            bool   `yaml:"enabled" json:"enabled"`
	NodeGroupLabel     string `yaml:"nodeGroupLabel" json:"nodeGroupLabel"`
	NodeSelector       string `yaml:"nodeSelector" json:"nodeSelector"`
	CrossZoneLatency   string `yaml:"crossZoneLatency" json:"crossZoneLatency"`
	CrossZoneBandwidth string `yaml:"crossZoneBandwidth" json:"crossZoneBandwidth"`
	BandwidthLimit     int    `yaml:"bandwidthLimit" json:"bandwidthLimit"`
	BandwidthBuffer    int    `yaml:"bandwidthBuffer" json:"bandwidthBuffer"`
	Jitter             string `yaml:"jitter" json:"jitter"`
	Correlation        string `yaml:"correlation" json:"correlation"`
}

type ApplicationConfig struct {
	Name          string `yaml:"name" json:"name"`
	Template      string `yaml:"template" json:"template"`
	Namespace     string `yaml:"namespace" json:"namespace"`
	Group         string `yaml:"group" json:"group"`
	SchedulerName string `yaml:"schedulerName" json:"schedulerName"`
	ProxyNodes    string `yaml:"proxyNodes" json:"proxyNodes"`
	MinReplicas   int    `yaml:"minReplicas" json:"minReplicas"`
	ProxyNodePort int    `yaml:"proxyNodePort" json:"proxyNodePort"`
}

type LoadGenConfig struct {
	Config map[string]any `yaml:"config" json:"config"`
}

func Load(path string) (*Experiment, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving experiment path: %w", err)
	}
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("reading experiment: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var experiment Experiment
	if err := decoder.Decode(&experiment); err != nil {
		return nil, fmt.Errorf("decoding experiment: %w", err)
	}
	experiment.SourceDir = filepath.Dir(absolutePath)
	applyDefaults(&experiment)
	if err := experiment.Validate(); err != nil {
		return nil, err
	}
	return &experiment, nil
}

func applyDefaults(experiment *Experiment) {
	if experiment.Runs == 0 {
		experiment.Runs = 1
	}
	if experiment.Commands.ProxmoxK3s == "" {
		experiment.Commands.ProxmoxK3s = "proxmox-k3s"
	}
	if experiment.Commands.Kubectl == "" {
		experiment.Commands.Kubectl = "kubectl"
	}
	if experiment.Commands.Helm == "" {
		experiment.Commands.Helm = "helm"
	}
	if experiment.Commands.ChaosInjector == "" {
		experiment.Commands.ChaosInjector = "chaos-injector"
	}
	if experiment.Commands.LoadGen == "" {
		experiment.Commands.LoadGen = "load-gen"
	}
	if experiment.Lifecycle.Cluster == "" {
		experiment.Lifecycle.Cluster = ClusterLifecycleExisting
	}
	if experiment.Tools.Application.Namespace == "" {
		experiment.Tools.Application.Namespace = "default"
	}
	if experiment.Tools.Application.Group == "" {
		experiment.Tools.Application.Group = experiment.Tools.Application.Name
	}
	if experiment.Tools.Application.SchedulerName == "" {
		experiment.Tools.Application.SchedulerName = "default-scheduler"
	}
	if experiment.Tools.Application.ProxyNodes == "" {
		experiment.Tools.Application.ProxyNodes = "all"
	}
	if experiment.Tools.SchedulerPlugins.Release == "" {
		experiment.Tools.SchedulerPlugins.Release = "scheduler-plugins"
	}
	if experiment.Tools.SchedulerPlugins.Namespace == "" {
		experiment.Tools.SchedulerPlugins.Namespace = "scheduler-plugins"
	}
	if experiment.Tools.Descheduler.Chart == "" {
		experiment.Tools.Descheduler.Chart = "/opt/experiment-executor/charts/descheduler"
	}
	if experiment.Tools.Descheduler.Release == "" {
		experiment.Tools.Descheduler.Release = "descheduler"
	}
	if experiment.Tools.Descheduler.Namespace == "" {
		experiment.Tools.Descheduler.Namespace = "kube-system"
	}
	if experiment.Tools.Descheduler.Interval == "" {
		experiment.Tools.Descheduler.Interval = "5m"
	}
	chaos := &experiment.Tools.ChaosInjector
	if chaos.NodeGroupLabel == "" {
		chaos.NodeGroupLabel = "topology.kubernetes.io/zone"
	}
	if chaos.CrossZoneLatency == "" {
		chaos.CrossZoneLatency = "50ms"
	}
	if chaos.CrossZoneBandwidth == "" {
		chaos.CrossZoneBandwidth = "10mbps"
	}
	if chaos.BandwidthLimit == 0 {
		chaos.BandwidthLimit = 20971520
	}
	if chaos.BandwidthBuffer == 0 {
		chaos.BandwidthBuffer = 10000
	}
	if chaos.Jitter == "" {
		chaos.Jitter = "0ms"
	}
	if chaos.Correlation == "" {
		chaos.Correlation = "0"
	}
}

func (experiment *Experiment) Validate() error {
	var problems []string
	if !validName.MatchString(experiment.Name) {
		problems = append(problems, "name must be a lowercase DNS-style name")
	}
	if experiment.Runs < 1 {
		problems = append(problems, "runs must be at least 1")
	}
	switch experiment.Lifecycle.Cluster {
	case ClusterLifecycleRecreate, ClusterLifecycleReuse, ClusterLifecycleExisting:
	default:
		problems = append(problems, "lifecycle.cluster must be recreate, reuse, or existing")
	}
	problems = append(problems, experiment.validateTools("tools", experiment.Tools)...)
	if len(problems) > 0 {
		return fmt.Errorf("invalid experiment:\n  - %s", strings.Join(problems, "\n  - "))
	}
	return nil
}

func (experiment *Experiment) validateTools(prefix string, tools ToolConfig) []string {
	var problems []string
	requireFile := func(field, path string) {
		if path == "" {
			problems = append(problems, field+" is required")
			return
		}
		if info, err := os.Stat(experiment.ResolvePath(path)); err != nil || info.IsDir() {
			problems = append(problems, field+" does not reference an existing file")
		}
	}
	requireDuration := func(field, value string) {
		if duration, err := time.ParseDuration(value); err != nil || duration <= 0 {
			problems = append(problems, field+" must be a positive duration")
		}
	}

	if len(tools.ProxmoxK3s.Config) == 0 {
		problems = append(problems, prefix+".proxmoxK3s.config is required")
	}
	if tools.MonAgent.Enabled {
		if tools.MonAgent.Version == "" {
			problems = append(problems, prefix+".monAgent.version is required when enabled")
		}
		requireDuration(prefix+".monAgent.scrapeInterval", tools.MonAgent.ScrapeInterval)
		requireDuration(prefix+".monAgent.metricsRange", tools.MonAgent.MetricsRange)
	}
	if tools.Mentat.Enabled {
		if tools.Mentat.Version == "" {
			problems = append(problems, prefix+".mentat.version is required when enabled")
		}
		requireDuration(prefix+".mentat.probeInterval", tools.Mentat.ProbeInterval)
	}
	if tools.SchedulerPlugins.Enabled {
		if tools.SchedulerPlugins.Chart == "" {
			problems = append(problems, prefix+".schedulerPlugins.chart is required when enabled")
		}
	}
	if tools.Descheduler.Enabled {
		if tools.Descheduler.Chart == "" {
			problems = append(problems, prefix+".descheduler.chart is required when enabled")
		}
		if len(tools.Descheduler.Policy) == 0 {
			problems = append(problems, prefix+".descheduler.policy is required when enabled")
		}
		if tools.Descheduler.Interval != "" {
			requireDuration(prefix+".descheduler.interval", tools.Descheduler.Interval)
		}
	}
	if tools.ChaosInjector.Enabled {
		if duration, err := time.ParseDuration(tools.ChaosInjector.CrossZoneLatency); err != nil || duration <= 0 {
			problems = append(problems, prefix+".chaosInjector.crossZoneLatency must be a positive duration")
		}
		if duration, err := time.ParseDuration(tools.ChaosInjector.Jitter); err != nil || duration < 0 {
			problems = append(problems, prefix+".chaosInjector.jitter must be a non-negative duration")
		}
		if tools.ChaosInjector.CrossZoneBandwidth == "" {
			problems = append(problems, prefix+".chaosInjector.crossZoneBandwidth is required when enabled")
		}
		if tools.ChaosInjector.BandwidthLimit < 1 || uint64(tools.ChaosInjector.BandwidthLimit) > uint64(^uint32(0)) {
			problems = append(problems, prefix+".chaosInjector.bandwidthLimit must be between 1 and 4294967295")
		}
		if tools.ChaosInjector.BandwidthBuffer < 1 || uint64(tools.ChaosInjector.BandwidthBuffer) > uint64(^uint32(0)) {
			problems = append(problems, prefix+".chaosInjector.bandwidthBuffer must be between 1 and 4294967295")
		}
	}
	requireFile(prefix+".application.template", tools.Application.Template)
	if tools.Application.Name == "" {
		problems = append(problems, prefix+".application.name is required")
	}
	if !validName.MatchString(tools.Application.Namespace) {
		problems = append(problems, prefix+".application.namespace must be a lowercase DNS-style name")
	}
	if !validName.MatchString(tools.Application.Group) {
		problems = append(problems, prefix+".application.group must be a lowercase DNS-style name")
	}
	if tools.Application.SchedulerName != "default-scheduler" && !tools.SchedulerPlugins.Enabled {
		problems = append(problems, prefix+".application.schedulerName requires schedulerPlugins.enabled")
	}
	if tools.Application.ProxyNodes != "all" && tools.Application.ProxyNodes != "workers" && tools.Application.ProxyNodes != "none" {
		problems = append(problems, prefix+".application.proxyNodes must be all, workers, or none")
	}
	if len(tools.LoadGen.Config) == 0 {
		problems = append(problems, prefix+".loadGen.config is required")
	} else if kubernetes, ok := tools.LoadGen.Config["kubernetes"].(map[string]any); ok {
		if enabled, _ := kubernetes["enabled"].(bool); enabled {
			namespace, _ := kubernetes["namespace"].(string)
			if namespace != tools.Application.Namespace {
				problems = append(problems, prefix+".loadGen.config.kubernetes.namespace must match application.namespace")
			}
		}
	}
	return problems
}

func (experiment *Experiment) ResolvePath(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(experiment.SourceDir, path))
}

func (experiment *Experiment) ResolveCommand(command string) string {
	if strings.ContainsRune(command, filepath.Separator) {
		return experiment.ResolvePath(command)
	}
	return command
}

func (experiment *Experiment) ConfigDir() string {
	return filepath.Join(experiment.SourceDir, "config")
}

func (experiment *Experiment) RunsDir() string {
	return filepath.Join(experiment.SourceDir, "runs")
}

func (tools ToolConfig) ApplicationNamespaceLabels() map[string]string {
	labels := make(map[string]string)
	if tools.MonAgent.Enabled {
		labels["mon-agent/enabled"] = "true"
	}
	if istioEnabled(tools.ProxmoxK3s.Config) {
		labels["istio-injection"] = "enabled"
	}
	return labels
}

func istioEnabled(proxmoxConfig map[string]any) bool {
	clusters, ok := proxmoxConfig["clusters"].([]any)
	if !ok {
		return false
	}
	for _, rawCluster := range clusters {
		cluster, ok := rawCluster.(map[string]any)
		if !ok {
			continue
		}
		addons, ok := cluster["addons"].(map[string]any)
		if !ok {
			continue
		}
		istio, ok := addons["istio"].(map[string]any)
		if !ok {
			continue
		}
		if enabled, _ := istio["enabled"].(bool); enabled {
			return true
		}
	}
	return false
}
