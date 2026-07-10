package config

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	Cluster        string `yaml:"cluster" json:"cluster"`
	KubeconfigPath string `yaml:"kubeconfigPath" json:"kubeconfigPath"`
}

type ToolConfig struct {
	ProxmoxK3s       ProxmoxK3sConfig       `yaml:"proxmoxK3s" json:"proxmoxK3s"`
	SchedulerPlugins SchedulerPluginsConfig `yaml:"schedulerPlugins" json:"schedulerPlugins"`
	Descheduler      DeschedulerConfig      `yaml:"descheduler" json:"descheduler"`
	ChaosInjector    ChaosInjectorConfig    `yaml:"chaosInjector" json:"chaosInjector"`
	Application      ApplicationConfig      `yaml:"application" json:"application"`
	LoadGen          LoadGenConfig          `yaml:"loadGen" json:"loadGen"`
}

type ProxmoxK3sConfig struct {
	Config          map[string]any         `yaml:"config" json:"config"`
	ApplicationPool *ApplicationPoolConfig `yaml:"applicationPool,omitempty" json:"applicationPool,omitempty"`
}

type ApplicationPoolConfig struct {
	NamePrefix string                  `yaml:"namePrefix" json:"namePrefix"`
	Defaults   ApplicationNodeDefaults `yaml:"defaults" json:"defaults"`
	Zones      []ApplicationZoneConfig `yaml:"zones" json:"zones"`
}

type ApplicationNodeDefaults struct {
	Template    string `yaml:"template" json:"template"`
	ProxmoxNode string `yaml:"proxmoxNode" json:"proxmoxNode"`
	Storage     string `yaml:"storage" json:"storage"`
	DiskSize    int    `yaml:"diskSize" json:"diskSize"`
	Gateway     string `yaml:"gateway" json:"gateway"`
	DNS         string `yaml:"dns" json:"dns"`
	SubnetMask  int    `yaml:"subnetMask" json:"subnetMask"`
}

type ApplicationZoneConfig struct {
	Name     string `yaml:"name" json:"name"`
	Count    int    `yaml:"count" json:"count"`
	IPStart  string `yaml:"ipStart" json:"ipStart"`
	Cores    int    `yaml:"cores" json:"cores"`
	Memory   int    `yaml:"memory" json:"memory"`
	DiskSize int    `yaml:"diskSize,omitempty" json:"diskSize,omitempty"`
}

func (c ProxmoxK3sConfig) MonAgent() MonAgentConfig {
	clusters, ok := c.Config["clusters"].([]any)
	if !ok {
		return MonAgentConfig{}
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
		raw, ok := addons["mon_agent"].(map[string]any)
		if !ok {
			continue
		}
		cfg := MonAgentConfig{}
		cfg.Enabled, _ = raw["enabled"].(bool)
		cfg.Version, _ = raw["version"].(string)
		cfg.MetricsRange, _ = raw["promql_range"].(string)
		switch s := raw["scrape_period_seconds"].(type) {
		case int:
			cfg.ScrapeInterval = fmt.Sprintf("%ds", s)
		case float64:
			cfg.ScrapeInterval = fmt.Sprintf("%ds", int(s))
		}
		return cfg
	}
	return MonAgentConfig{}
}

type MonAgentConfig struct {
	Enabled        bool
	Version        string
	ScrapeInterval string
	MetricsRange   string
}

func (c ProxmoxK3sConfig) Mentat() MentatConfig {
	clusters, ok := c.Config["clusters"].([]any)
	if !ok {
		return MentatConfig{}
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
		raw, ok := addons["mentat"].(map[string]any)
		if !ok {
			continue
		}
		cfg := MentatConfig{}
		cfg.Enabled, _ = raw["enabled"].(bool)
		cfg.Version, _ = raw["version"].(string)
		switch s := raw["sleep_seconds"].(type) {
		case int:
			cfg.ProbeInterval = fmt.Sprintf("%ds", s)
		case float64:
			cfg.ProbeInterval = fmt.Sprintf("%ds", int(s))
		}
		return cfg
	}
	return MentatConfig{}
}

type MentatConfig struct {
	Enabled       bool
	Version       string
	ProbeInterval string
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
	Enabled                          bool   `yaml:"enabled" json:"enabled"`
	NodeGroupLabel                   string `yaml:"nodeGroupLabel" json:"nodeGroupLabel"`
	NodeSelector                     string `yaml:"nodeSelector" json:"nodeSelector"`
	NetworkInterface                 string `yaml:"networkInterface" json:"networkInterface"`
	EnableLatency                    *bool  `yaml:"enableLatency,omitempty" json:"enableLatency,omitempty"`
	EnableBandwidth                  *bool  `yaml:"enableBandwidth,omitempty" json:"enableBandwidth,omitempty"`
	EnablePacketLoss                 *bool  `yaml:"enablePacketLoss,omitempty" json:"enablePacketLoss,omitempty"`
	CrossZoneLatency                 string `yaml:"crossZoneLatency" json:"crossZoneLatency"`
	CrossZoneBandwidthBytesPerSecond string `yaml:"crossZoneBandwidthBytesPerSecond" json:"crossZoneBandwidthBytesPerSecond"`
	PacketLoss                       string `yaml:"packetLoss" json:"packetLoss"`
	Jitter                           string `yaml:"jitter" json:"jitter"`
	Correlation                      string `yaml:"correlation" json:"correlation"`
}

type ApplicationConfig struct {
	Name                string    `yaml:"name" json:"name"`
	Template            string    `yaml:"template" json:"template"`
	Namespace           string    `yaml:"namespace" json:"namespace"`
	Group               string    `yaml:"group" json:"group"`
	SchedulerName       string    `yaml:"schedulerName" json:"schedulerName"`
	ProxyNodes          string    `yaml:"proxyNodes" json:"proxyNodes"`
	MinReplicas         int       `yaml:"minReplicas" json:"minReplicas"`
	ProxyNodePort       int       `yaml:"proxyNodePort" json:"proxyNodePort"`
	PortBind            int       `yaml:"portbind,omitempty" json:"portbind,omitempty"`
	P2PToken            string    `yaml:"p2pToken,omitempty" json:"p2pToken,omitempty"`
	MasterHostname      string    `yaml:"masterHostname,omitempty" json:"masterHostname,omitempty"`
	UseGPU              bool      `yaml:"useGPU,omitempty" json:"useGPU,omitempty"`
	NumWorker           int       `yaml:"numWorker,omitempty" json:"numWorker,omitempty"`
	WorkerBasePort      int       `yaml:"workerBasePort,omitempty" json:"workerBasePort,omitempty"`
	WorkerNodeName      string    `yaml:"workerNodeName,omitempty" json:"workerNodeName,omitempty"`
	WorkerMemoryLimitGi int       `yaml:"workerMemoryLimitGi,omitempty" json:"workerMemoryLimitGi,omitempty"`
	GatewayNodePort     int       `yaml:"gatewayNodePort,omitempty" json:"gatewayNodePort,omitempty"`
	HPA                 HPAConfig `yaml:"hpa" json:"hpa"`
	CPA                 CPAConfig `yaml:"cpa" json:"cpa"`
}

type HPAConfig struct {
	Enabled                        bool `yaml:"enabled" json:"enabled"`
	MinReplicas                    int  `yaml:"minReplicas" json:"minReplicas"`
	MaxReplicas                    int  `yaml:"maxReplicas" json:"maxReplicas"`
	TargetCPUUtilizationPercentage int  `yaml:"targetCPUUtilizationPercentage" json:"targetCPUUtilizationPercentage"`
}

type CPAConfig struct {
	Enabled                  bool    `yaml:"enabled" json:"enabled"`
	Image                    string  `yaml:"image" json:"image"`
	ImagePullPolicy          string  `yaml:"imagePullPolicy" json:"imagePullPolicy"`
	IntervalMillis           int     `yaml:"intervalMillis" json:"intervalMillis"`
	MinReplicas              int     `yaml:"minReplicas" json:"minReplicas"`
	MaxReplicas              int     `yaml:"maxReplicas" json:"maxReplicas"`
	PrometheusURL            string  `yaml:"prometheusURL" json:"prometheusURL"`
	TargetResponseTimeMillis float64 `yaml:"targetResponseTimeMillis" json:"targetResponseTimeMillis"`
	TargetPercentage         float64 `yaml:"targetPercentage" json:"targetPercentage"`
	TimeRange                string  `yaml:"timeRange" json:"timeRange"`
	RedisImage               string  `yaml:"redisImage" json:"redisImage"`
	RedisHost                string  `yaml:"redisHost" json:"redisHost"`
	KP                       float64 `yaml:"kp" json:"kp"`
	KI                       float64 `yaml:"ki" json:"ki"`
	KD                       float64 `yaml:"kd" json:"kd"`
	DownscaleStabilization   int     `yaml:"downscaleStabilization" json:"downscaleStabilization"`
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
	if pool := experiment.Tools.ProxmoxK3s.ApplicationPool; pool != nil && pool.NamePrefix == "" {
		pool.NamePrefix = "app"
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
	app := &experiment.Tools.Application
	if app.HPA.MinReplicas == 0 {
		app.HPA.MinReplicas = app.MinReplicas
	}
	if app.HPA.MinReplicas == 0 {
		app.HPA.MinReplicas = 1
	}
	if app.HPA.MaxReplicas == 0 {
		app.HPA.MaxReplicas = 10
	}
	if app.HPA.TargetCPUUtilizationPercentage == 0 {
		app.HPA.TargetCPUUtilizationPercentage = 70
	}
	if app.CPA.ImagePullPolicy == "" {
		app.CPA.ImagePullPolicy = "IfNotPresent"
	}
	if app.CPA.IntervalMillis == 0 {
		app.CPA.IntervalMillis = 15000
	}
	if app.CPA.MinReplicas == 0 {
		app.CPA.MinReplicas = app.MinReplicas
	}
	if app.CPA.MinReplicas == 0 {
		app.CPA.MinReplicas = 1
	}
	if app.CPA.MaxReplicas == 0 {
		app.CPA.MaxReplicas = 10
	}
	if app.CPA.PrometheusURL == "" {
		app.CPA.PrometheusURL = "http://prometheus-kube-prometheus-prometheus.observability:9090/api/v1/query"
	}
	if app.CPA.TargetResponseTimeMillis == 0 {
		app.CPA.TargetResponseTimeMillis = 250
	}
	if app.CPA.TargetPercentage == 0 {
		app.CPA.TargetPercentage = 0.95
	}
	if app.CPA.TimeRange == "" {
		app.CPA.TimeRange = "1m"
	}
	if app.CPA.RedisImage == "" {
		app.CPA.RedisImage = "redis:7.4-alpine"
	}
	if app.CPA.RedisHost == "" {
		app.CPA.RedisHost = "custom-pod-autoscaler-redis"
	}
	if app.CPA.KP == 0 {
		app.CPA.KP = 1
	}
	if app.CPA.DownscaleStabilization == 0 {
		app.CPA.DownscaleStabilization = 300
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
	if chaos.EnableLatency == nil {
		value := true
		chaos.EnableLatency = &value
	}
	if chaos.EnableBandwidth == nil {
		value := false
		chaos.EnableBandwidth = &value
	}
	if chaos.EnablePacketLoss == nil {
		value := false
		chaos.EnablePacketLoss = &value
	}
	if chaos.CrossZoneLatency == "" {
		chaos.CrossZoneLatency = "50ms"
	}
	if chaos.PacketLoss == "" {
		chaos.PacketLoss = "0"
	}
	if chaos.NetworkInterface == "" {
		chaos.NetworkInterface = "flannel.1"
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
	if pool := tools.ProxmoxK3s.ApplicationPool; pool != nil {
		if !validName.MatchString(pool.NamePrefix) {
			problems = append(problems, prefix+".proxmoxK3s.applicationPool.namePrefix must be a lowercase DNS-style name")
		}
		if pool.Defaults.Template == "" || pool.Defaults.ProxmoxNode == "" || pool.Defaults.Storage == "" || pool.Defaults.Gateway == "" || pool.Defaults.DNS == "" || pool.Defaults.DiskSize < 1 || pool.Defaults.SubnetMask < 1 || pool.Defaults.SubnetMask > 32 {
			problems = append(problems, prefix+".proxmoxK3s.applicationPool.defaults is incomplete")
		}
		seenZones := map[string]bool{}
		for i, zone := range pool.Zones {
			field := fmt.Sprintf("%s.proxmoxK3s.applicationPool.zones[%d]", prefix, i)
			if !validName.MatchString(zone.Name) || seenZones[zone.Name] {
				problems = append(problems, field+".name must be unique and DNS-style")
			}
			seenZones[zone.Name] = true
			if zone.Count < 1 || zone.Cores < 1 || zone.Memory < 1 {
				problems = append(problems, field+" count, cores, and memory must be positive")
			}
			if ip := net.ParseIP(zone.IPStart); ip == nil || ip.To4() == nil {
				problems = append(problems, field+".ipStart must be an IPv4 address")
			}
		}
		if len(pool.Zones) == 0 {
			problems = append(problems, prefix+".proxmoxK3s.applicationPool.zones must not be empty")
		}
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
		latencyEnabled := tools.ChaosInjector.EnableLatency != nil && *tools.ChaosInjector.EnableLatency
		bandwidthEnabled := tools.ChaosInjector.EnableBandwidth != nil && *tools.ChaosInjector.EnableBandwidth
		packetLossEnabled := tools.ChaosInjector.EnablePacketLoss != nil && *tools.ChaosInjector.EnablePacketLoss
		if latencyEnabled {
			if duration, err := time.ParseDuration(tools.ChaosInjector.CrossZoneLatency); err != nil || duration <= 0 {
				problems = append(problems, prefix+".chaosInjector.crossZoneLatency must be a positive duration")
			}
		}
		if duration, err := time.ParseDuration(tools.ChaosInjector.Jitter); err != nil || duration < 0 {
			problems = append(problems, prefix+".chaosInjector.jitter must be a non-negative duration")
		}
		if bandwidthEnabled {
			if _, err := parsePositiveFloat(tools.ChaosInjector.CrossZoneBandwidthBytesPerSecond); err != nil {
				problems = append(problems, prefix+".chaosInjector.crossZoneBandwidthBytesPerSecond must be a positive bytes-per-second value")
			}
		}
		if packetLossEnabled {
			if _, err := parsePercentage(tools.ChaosInjector.PacketLoss); err != nil {
				problems = append(problems, prefix+".chaosInjector.packetLoss must be a percentage from 0 to 100")
			}
		}
		if strings.TrimSpace(tools.ChaosInjector.NetworkInterface) == "" {
			problems = append(problems, prefix+".chaosInjector.networkInterface is required when enabled")
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
	if strings.TrimSpace(tools.Application.SchedulerName) == "" {
		problems = append(problems, prefix+".application.schedulerName must not be empty")
	}
	if tools.Application.ProxyNodes != "all" && tools.Application.ProxyNodes != "workers" && tools.Application.ProxyNodes != "none" {
		problems = append(problems, prefix+".application.proxyNodes must be all, workers, or none")
	}
	if tools.Application.HPA.Enabled && tools.Application.CPA.Enabled {
		problems = append(problems, prefix+".application must not enable both hpa and cpa")
	}
	if tools.Application.HPA.Enabled {
		if tools.Application.HPA.MinReplicas < 1 || tools.Application.HPA.MaxReplicas < tools.Application.HPA.MinReplicas {
			problems = append(problems, prefix+".application.hpa replicas must satisfy 1 <= minReplicas <= maxReplicas")
		}
		if tools.Application.HPA.TargetCPUUtilizationPercentage < 1 || tools.Application.HPA.TargetCPUUtilizationPercentage > 100 {
			problems = append(problems, prefix+".application.hpa.targetCPUUtilizationPercentage must be from 1 to 100")
		}
	}
	if tools.Application.CPA.Enabled {
		if tools.Application.CPA.Image == "" {
			problems = append(problems, prefix+".application.cpa.image is required when enabled")
		}
		if tools.Application.CPA.IntervalMillis < 1 {
			problems = append(problems, prefix+".application.cpa.intervalMillis must be positive")
		}
		if tools.Application.CPA.MinReplicas < 1 || tools.Application.CPA.MaxReplicas < tools.Application.CPA.MinReplicas {
			problems = append(problems, prefix+".application.cpa replicas must satisfy 1 <= minReplicas <= maxReplicas")
		}
		if tools.Application.CPA.PrometheusURL == "" || tools.Application.CPA.TimeRange == "" || tools.Application.CPA.RedisHost == "" {
			problems = append(problems, prefix+".application.cpa prometheusURL, timeRange, and redisHost are required when enabled")
		}
		if tools.Application.CPA.TargetResponseTimeMillis <= 0 {
			problems = append(problems, prefix+".application.cpa.targetResponseTimeMillis must be positive")
		}
		if tools.Application.CPA.TargetPercentage <= 0 || tools.Application.CPA.TargetPercentage > 1 {
			problems = append(problems, prefix+".application.cpa.targetPercentage must be from 0 to 1")
		}
		if tools.Application.CPA.DownscaleStabilization < 0 {
			problems = append(problems, prefix+".application.cpa.downscaleStabilization must be non-negative")
		}
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

func parsePercentage(value string) (float64, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(value), "%")
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || parsed < 0 || parsed > 100 {
		return 0, fmt.Errorf("invalid percentage")
	}
	return parsed, nil
}

func parsePositiveFloat(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid positive float")
	}
	return parsed, nil
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
	if tools.ProxmoxK3s.MonAgent().Enabled {
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
