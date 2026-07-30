package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAppliesDefaultsAndCountsRuns(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"app.tmpl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	experimentPath := filepath.Join(dir, "experiment.yaml")
	content := `name: baseline
runs: 3
tools:
      proxmoxK3s:
        config:
          clusters: []
      application:
        name: app
        template: app.tmpl
      loadGen:
        config:
          pattern: {type: constant, rps: 1, duration: 1m}
`
	if err := os.WriteFile(experimentPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	experiment, err := Load(experimentPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if experiment.Runs != 3 {
		t.Fatalf("runs = %d", experiment.Runs)
	}
	if experiment.Lifecycle.Cluster != ClusterLifecycleExisting {
		t.Fatalf("cluster lifecycle = %q", experiment.Lifecycle.Cluster)
	}
	if experiment.Tools.Application.SchedulerName != "default-scheduler" {
		t.Fatalf("scheduler name = %q", experiment.Tools.Application.SchedulerName)
	}
	if experiment.Tools.Application.ProxyNodes != "all" {
		t.Fatalf("proxy nodes = %q", experiment.Tools.Application.ProxyNodes)
	}
	if experiment.Tools.Application.Namespace != "default" {
		t.Fatalf("namespace = %q", experiment.Tools.Application.Namespace)
	}
	if experiment.Tools.Application.Group != "app" {
		t.Fatalf("group = %q", experiment.Tools.Application.Group)
	}
	if experiment.Tools.Application.Autoscaler != nil {
		t.Fatalf("autoscaler should default to nil: %#v", experiment.Tools.Application.Autoscaler)
	}
}

func TestLoadAppliesOnlineBoutiqueResourceRequestDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tmpl"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `name: online-boutique
tools:
  proxmoxK3s:
    config: {clusters: []}
  application:
    name: onlineboutique
    template: app.tmpl
  loadGen:
    config:
      pattern: {type: constant, rps: 1, duration: 1m}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	experiment, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if experiment.Tools.Application.CPURequest != "100m" || experiment.Tools.Application.MemoryRequest != "64Mi" {
		t.Fatalf("resource requests = cpu %q, memory %q", experiment.Tools.Application.CPURequest, experiment.Tools.Application.MemoryRequest)
	}
}

func TestLoadAllowsCustomSchedulersWhenPluginIsDisabled(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"app.tmpl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	content := `name: invalid
tools:
      proxmoxK3s:
        config: {clusters: []}
      application:
        name: app
        template: app.tmpl
        schedulerName: custom-scheduler
      loadGen:
        config:
          pattern: {type: constant, rps: 1, duration: 1m}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	experiment, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if experiment.Tools.Application.SchedulerName != "custom-scheduler" {
		t.Fatalf("schedulerName = %q", experiment.Tools.Application.SchedulerName)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "experiment.yaml")
	content := `name: test-experiment
unexpected: true
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "field unexpected not found") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadPreservesOpaqueAutoscalerConfiguration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tmpl"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `name: autoscaling
tools:
      proxmoxK3s:
        config: {clusters: []}
      application:
        name: app
        template: app.tmpl
        autoscaler:
          cpa:
            plugin: future-plugin
            config:
              arbitraryOption: [one, two]
      loadGen:
        config: {pattern: {type: constant}}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	experiment, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cpa, ok := experiment.Tools.Application.Autoscaler["cpa"].(map[string]any)
	if !ok || cpa["plugin"] != "future-plugin" {
		t.Fatalf("autoscaler configuration = %#v", experiment.Tools.Application.Autoscaler)
	}
}

func TestLoadDoesNotValidateAutoscalerSpecificConfiguration(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tmpl"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `name: autoscaling
tools:
  proxmoxK3s:
    config: {clusters: []}
  application:
    name: app
    template: app.tmpl
    autoscaler:
      hpa:
        config:
          implementationSpecific: true
  loadGen:
    config: {pattern: {type: constant}}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	experiment, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	hpa := experiment.Tools.Application.Autoscaler["hpa"].(map[string]any)
	config := hpa["config"].(map[string]any)
	if config["implementationSpecific"] != true {
		t.Fatalf("autoscaler configuration = %#v", config)
	}
}

func TestLoadAppliesChaosInjectorDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tmpl"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `name: chaos
tools:
      proxmoxK3s:
        config: {clusters: []}
      chaosInjector:
        enabled: true
      application:
        name: app
        template: app.tmpl
      loadGen:
        config: {pattern: {type: constant}}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	experiment, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	chaos := experiment.Tools.ChaosInjector
	if chaos.NodeGroupLabel != "topology.kubernetes.io/zone" || chaos.NetworkInterface != "flannel.1" || chaos.DefaultCrossZoneLatency != "50ms" {
		t.Fatalf("chaos defaults = %#v", chaos)
	}
	if chaos.DefaultCrossZonePacketLoss != "0" {
		t.Fatalf("chaos defaults = %#v", chaos)
	}
	if chaos.EnableLatency == nil || !*chaos.EnableLatency || chaos.EnableBandwidth == nil || *chaos.EnableBandwidth || chaos.EnablePacketLoss == nil || *chaos.EnablePacketLoss {
		t.Fatalf("chaos defaults = %#v", chaos)
	}
}

func TestLoadAcceptsChaosInjectorBandwidthAndPacketLoss(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tmpl"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `name: chaos
tools:
      proxmoxK3s:
        config: {clusters: []}
      chaosInjector:
        enabled: true
        hostNetwork: true
        enableLatency: false
        enableBandwidth: true
        defaultCrossZoneBandwidthBytesPerSecond: "1250000"
        enablePacketLoss: true
        defaultCrossZonePacketLoss: 1.5
      application:
        name: app
        template: app.tmpl
      loadGen:
        config: {pattern: {type: constant}}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	experiment, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	chaos := experiment.Tools.ChaosInjector
	if !chaos.HostNetwork || *chaos.EnableLatency || !*chaos.EnableBandwidth || !*chaos.EnablePacketLoss || chaos.DefaultCrossZoneBandwidthBytesPerSecond != "1250000" || chaos.DefaultCrossZonePacketLoss != "1.5" {
		t.Fatalf("chaos config = %#v", chaos)
	}
}

func TestLoadAcceptsZoneLinkRules(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tmpl"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `name: chaos
tools:
  proxmoxK3s:
    config: {clusters: []}
  chaosInjector:
    enabled: true
    zoneLinks:
      - from: cloud
        to: fog
        latency: 20ms
        bandwidthBytesPerSecond: "10000000"
        packetLoss: "0.1"
        bidirectional: true
      - from: fog
        to: edge-a
        latency: 8ms
  application:
    name: app
    template: app.tmpl
  loadGen:
    config: {pattern: {type: constant}}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	experiment, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	rules := experiment.Tools.ChaosInjector.ZoneLinks
	if len(rules) != 2 || rules[0].From != "cloud" || rules[0].To != "fog" || rules[0].Latency != "20ms" || rules[0].BandwidthBytesPerSecond != "10000000" || rules[0].PacketLoss != "0.1" || !rules[0].Bidirectional {
		t.Fatalf("zone link rules = %#v", rules)
	}
}

func TestLoadRejectsNoOpChaosInjector(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tmpl"), []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := `name: chaos
tools:
      proxmoxK3s:
        config: {clusters: []}
      chaosInjector:
        enabled: true
        enableLatency: false
        enableBandwidth: false
        enablePacketLoss: false
      application:
        name: app
        template: app.tmpl
      loadGen:
        config: {pattern: {type: constant}}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "must enable at least one impairment") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestApplicationNamespaceLabelsFollowEnabledAddons(t *testing.T) {
	tools := ToolConfig{
		ProxmoxK3s: ProxmoxK3sConfig{Config: map[string]any{
			"clusters": []any{map[string]any{
				"addons": map[string]any{
					"istio":     map[string]any{"enabled": true},
					"mon_agent": map[string]any{"enabled": true},
				},
			}},
		}},
	}

	labels := tools.ApplicationNamespaceLabels()
	if labels["mon-agent/enabled"] != "true" || labels["istio-injection"] != "enabled" {
		t.Fatalf("labels = %#v", labels)
	}
}
