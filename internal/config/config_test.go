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
	if experiment.Tools.Application.HPA.Enabled || experiment.Tools.Application.CPA.Enabled {
		t.Fatalf("autoscalers should default disabled: hpa=%#v cpa=%#v", experiment.Tools.Application.HPA, experiment.Tools.Application.CPA)
	}
	if experiment.Tools.Application.HPA.MaxReplicas != 10 || experiment.Tools.Application.CPA.MaxReplicas != 10 {
		t.Fatalf("autoscaler defaults not applied: hpa=%#v cpa=%#v", experiment.Tools.Application.HPA, experiment.Tools.Application.CPA)
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

func TestLoadRejectsBothApplicationAutoscalersEnabled(t *testing.T) {
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
        hpa:
          enabled: true
        cpa:
          enabled: true
          image: custom-pod-autoscaler:latest
      loadGen:
        config: {pattern: {type: constant}}
`
	path := filepath.Join(dir, "experiment.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "must not enable both hpa and cpa") {
		t.Fatalf("Load() error = %v", err)
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
	if chaos.NodeGroupLabel != "topology.kubernetes.io/zone" || chaos.NetworkInterface != "flannel.1" || chaos.CrossZoneLatency != "50ms" {
		t.Fatalf("chaos defaults = %#v", chaos)
	}
	if chaos.Jitter != "0ms" || chaos.Correlation != "0" || chaos.PacketLoss != "0" {
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
        enableLatency: false
        enableBandwidth: true
        crossZoneBandwidthBytesPerSecond: "1250000"
        enablePacketLoss: true
        packetLoss: 1.5
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
	if *chaos.EnableLatency || !*chaos.EnableBandwidth || !*chaos.EnablePacketLoss || chaos.CrossZoneBandwidthBytesPerSecond != "1250000" || chaos.PacketLoss != "1.5" {
		t.Fatalf("chaos config = %#v", chaos)
	}
}

func TestLoadAcceptsNoOpChaosInjector(t *testing.T) {
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
	experiment, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	chaos := experiment.Tools.ChaosInjector
	if *chaos.EnableLatency || *chaos.EnableBandwidth || *chaos.EnablePacketLoss {
		t.Fatalf("chaos config = %#v", chaos)
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
