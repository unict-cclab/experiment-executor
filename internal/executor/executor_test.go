package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unict-cclab/experiment-executor/internal/config"
	"gopkg.in/yaml.v3"
)

func TestDryRunRendersArtifactsWithoutExternalCommands(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "application.yaml.tmpl")
	if err := os.WriteFile(templatePath, []byte("group: {{ .group }}\nscheduler: {{ .schedulerName }}\n{{ range .proxyNodes }}node: {{ . }}\n{{ end }}"), 0o600); err != nil {
		t.Fatal(err)
	}
	locustfile := filepath.Join(dir, "locustfile.py")
	if err := os.WriteFile(locustfile, []byte("# test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "kubeconfig"), []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	experiment := &config.Experiment{
		Name:      "dry-run",
		Runs:      1,
		SourceDir: dir,
		Commands: config.Commands{
			ProxmoxK3s: "proxmox-k3s",
			Kubectl:    "kubectl",
			Helm:       "helm",
			LoadGen:    "load-gen",
		},
		Lifecycle: config.ExperimentLifecycle{Cluster: config.ClusterLifecycleExisting},
		Tools: config.ToolConfig{
			ProxmoxK3s: config.ProxmoxK3sConfig{Config: map[string]any{
				"clusters": []any{map[string]any{
					"name":            "test",
					"kubeconfig_path": "kubeconfig",
					"control_plane":   []any{map[string]any{"name": "cp-01", "ip": "192.0.2.1"}},
					"workers":         []any{map[string]any{"name": "worker-01", "ip": "192.0.2.2"}},
				}},
			}},
			Application: config.ApplicationConfig{
				Name: "app", Template: templatePath, Namespace: "default", Group: "app",
				SchedulerName: "custom-scheduler", ProxyNodes: "all",
			},
			Descheduler: config.DeschedulerConfig{
				Enabled: true, Chart: "/opt/experiment-executor/charts/descheduler", Release: "descheduler", Namespace: "kube-system", Interval: "30s",
				Policy: map[string]any{"apiVersion": "descheduler/v1alpha2", "kind": "DeschedulerPolicy", "profiles": []any{}},
			},
			LoadGen: config.LoadGenConfig{Config: map[string]any{
				"locustfile": locustfile,
				"pattern":    map[string]any{"type": "constant", "rps": 1, "duration": "1s"},
			}},
		},
	}

	if err := Run(context.Background(), experiment, Options{DryRun: true}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	runDir := filepath.Join(dir, "runs", "run-001")
	for _, path := range []string{"run.json"} {
		if _, err := os.Stat(filepath.Join(runDir, path)); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	for _, path := range []string{"experiment.resolved.yaml", "proxmox-k3s.yaml", "kubeconfig", "descheduler-values.yaml", "application.yaml", "load-gen.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, "config", path)); err != nil {
			t.Fatalf("missing config/%s: %v", path, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state runState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Status != "dry-run" {
		t.Fatalf("status = %q", state.Status)
	}
	application, err := os.ReadFile(filepath.Join(dir, "config", "application.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(application)
	for _, want := range []string{
		"scheduler: custom-scheduler",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered application missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderOnlineBoutiqueAutoscalers(t *testing.T) {
	templatePath, err := filepath.Abs(filepath.Join("..", "..", "..", "benchmark-apps", "onlineboutique", "templates", "manifest.yaml.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		app       config.ApplicationConfig
		want      string
		wantCount int
	}{
		{
			name: "hpa",
			app: config.ApplicationConfig{
				Name: "onlineboutique", Template: templatePath, Namespace: "default", Group: "onlineboutique",
				SchedulerName: "default-scheduler", MinReplicas: 2,
				HPA: config.HPAConfig{Enabled: true, MinReplicas: 2, MaxReplicas: 10, TargetCPUUtilizationPercentage: 70},
			},
			want:      "kind: HorizontalPodAutoscaler",
			wantCount: 11,
		},
		{
			name: "cpa",
			app: config.ApplicationConfig{
				Name: "onlineboutique", Template: templatePath, Namespace: "default", Group: "onlineboutique",
				SchedulerName: "default-scheduler", MinReplicas: 2,
				CPA: config.CPAConfig{
					Enabled: true, Image: "custom-pod-autoscaler:latest", ImagePullPolicy: "IfNotPresent",
					IntervalMillis: 15000, MinReplicas: 2, MaxReplicas: 10,
					PrometheusURL: "http://prometheus/api/v1/query", TargetResponseTimeMillis: 250, TargetPercentage: 0.95,
					TimeRange: "1m", RedisImage: "redis:7.4-alpine", RedisHost: "custom-pod-autoscaler-redis",
					KP: 1, DownscaleStabilization: 300,
				},
			},
			want:      "kind: CustomPodAutoscaler",
			wantCount: 10,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			experiment := &config.Experiment{SourceDir: dir, Tools: config.ToolConfig{Application: tc.app}}
			runner := &Runner{experiment: experiment}
			files := runFiles{application: filepath.Join(dir, "application.yaml")}
			if err := runner.renderApplication(*experiment, files, nil); err != nil {
				t.Fatalf("renderApplication() error = %v", err)
			}
			data, err := os.ReadFile(files.application)
			if err != nil {
				t.Fatal(err)
			}
			rendered := string(data)
			if count := strings.Count(rendered, tc.want); count != tc.wantCount {
				t.Fatalf("count(%q) = %d, want %d", tc.want, count, tc.wantCount)
			}
		})
	}
}

func TestRenderLocalAIApplication(t *testing.T) {
	templatePath, err := filepath.Abs(filepath.Join("..", "..", "..", "benchmark-apps", "localai", "templates", "manifest.yaml.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	experiment := &config.Experiment{
		SourceDir: dir,
		Tools: config.ToolConfig{Application: config.ApplicationConfig{
			Name: "localai", Template: templatePath, Namespace: "local-ai", Group: "cluster-1",
			SchedulerName: "default-scheduler", ProxyNodes: "workers",
			Parameters: map[string]any{
				"group": "must-not-override", "portbind": 8080, "p2pToken": "test-token",
				"masterHostname": "", "useGPU": false, "numWorker": 3,
				"workerBasePort": 19100, "workerNodeName": "", "workerMemoryLimitGi": 0,
				"gatewayNodePort": 30080,
			},
		}},
	}
	runner := &Runner{experiment: experiment}
	files := runFiles{application: filepath.Join(dir, "application.yaml")}
	nodes := []nodeInfo{{Name: "control-plane", Worker: false}, {Name: "worker-01", Worker: true}}
	if err := runner.renderApplication(*experiment, files, nodes); err != nil {
		t.Fatalf("renderApplication() error = %v", err)
	}
	data, err := os.ReadFile(files.application)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(data)
	for _, expected := range []string{
		"name: local-ai-cluster-1", "schedulerName: default-scheduler", "--p2ptoken",
		"test-token", "nodePort: 30080", "name: gateway-cluster-1-worker-01",
	} {
		if !strings.Contains(rendered, expected) {
			t.Errorf("rendered LocalAI manifest does not contain %q", expected)
		}
	}
	if count := strings.Count(rendered, "name: localai-worker-cluster-1-"); count != 3 {
		t.Errorf("rendered %d LocalAI workers, want 3", count)
	}
	if strings.Contains(rendered, "must-not-override") {
		t.Error("application parameters overrode executor-owned group")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	for document := 1; ; document++ {
		var value any
		if err := decoder.Decode(&value); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("rendered LocalAI YAML document %d is invalid: %v", document, err)
		}
	}
}

func TestExpandApplicationPoolUsesSequentialStaticAddresses(t *testing.T) {
	value := map[string]any{"clusters": []any{map[string]any{"workers": []any{map[string]any{"name": "management", "ip": "192.0.2.10"}}}}}
	pool := &config.ApplicationPoolConfig{
		NamePrefix: "app",
		Defaults:   config.ApplicationNodeDefaults{Template: "ubuntu", ProxmoxNode: "pve", Storage: "zfs", DiskSize: 40, Gateway: "192.0.2.1", DNS: "192.0.2.2", SubnetMask: 24},
		Zones:      []config.ApplicationZoneConfig{{Name: "zone-a", Count: 2, IPStart: "192.0.2.20", Cores: 4, Memory: 8192}},
	}
	if err := expandApplicationPool(value, pool); err != nil {
		t.Fatal(err)
	}
	nodes := declaredNodes(value)
	if len(nodes) != 3 || nodes[1].Name != "app-zone-a-01" || nodes[1].InternalIP != "192.0.2.20" || nodes[2].InternalIP != "192.0.2.21" || nodes[2].Zone != "zone-a" || nodes[2].Pool != "app" {
		t.Fatalf("expanded nodes = %#v", nodes)
	}
}

func TestConfigureObservabilityPreservesProxmoxDefaultsWhenSettingsAreOmitted(t *testing.T) {
	dir := t.TempDir()
	kubectl := fakeCommand(t, dir, "kubectl")
	files := runFiles{
		logs:       filepath.Join(dir, "logs"),
		kubeconfig: filepath.Join(dir, "kubeconfig"),
	}
	if err := os.MkdirAll(files.logs, 0o755); err != nil {
		t.Fatal(err)
	}
	experiment := config.Experiment{
		Commands: config.Commands{Kubectl: kubectl},
		Tools: config.ToolConfig{ProxmoxK3s: config.ProxmoxK3sConfig{Config: map[string]any{
			"clusters": []any{map[string]any{
				"addons": map[string]any{
					"mon_agent": map[string]any{"enabled": true},
					"mentat":    map[string]any{"enabled": true},
				},
			}},
		}}},
	}
	runner := &Runner{experiment: &experiment}

	if err := runner.configureObservability(context.Background(), experiment, files); err != nil {
		t.Fatalf("configureObservability() error = %v", err)
	}

	for _, unexpected := range []string{"mon-agent-image.log", "mon-agent-config.log", "mentat-image.log", "mentat-config.log"} {
		if _, err := os.Stat(filepath.Join(files.logs, unexpected)); !os.IsNotExist(err) {
			t.Fatalf("unexpected %s: %v", unexpected, err)
		}
	}
	if _, err := os.Stat(filepath.Join(files.logs, "mentat-rbac.log")); err != nil {
		t.Fatalf("missing mentat RBAC patch log: %v", err)
	}
}

func TestConfigureObservabilityPatchesExplicitSettings(t *testing.T) {
	dir := t.TempDir()
	kubectl := fakeCommand(t, dir, "kubectl")
	files := runFiles{
		logs:       filepath.Join(dir, "logs"),
		kubeconfig: filepath.Join(dir, "kubeconfig"),
	}
	if err := os.MkdirAll(files.logs, 0o755); err != nil {
		t.Fatal(err)
	}
	experiment := config.Experiment{
		Commands: config.Commands{Kubectl: kubectl},
		Tools: config.ToolConfig{ProxmoxK3s: config.ProxmoxK3sConfig{Config: map[string]any{
			"clusters": []any{map[string]any{
				"addons": map[string]any{
					"mon_agent": map[string]any{"enabled": true, "version": "v1.2.3", "scrape_period_seconds": 45, "promql_range": "10m"},
					"mentat":    map[string]any{"enabled": true, "version": "v2.3.4", "sleep_seconds": 7},
				},
			}},
		}}},
	}
	runner := &Runner{experiment: &experiment}

	if err := runner.configureObservability(context.Background(), experiment, files); err != nil {
		t.Fatalf("configureObservability() error = %v", err)
	}

	assertLogContains(t, files.logs, "mon-agent-image.log", "mon-agent=ghcr.io/unict-cclab/mon-agent:v1.2.3")
	assertLogContains(t, files.logs, "mon-agent-config.log", "SCRAPE_PERIOD_SECONDS=45", "PROMQL_RANGE=10m")
	assertLogContains(t, files.logs, "mentat-image.log", "mentat=ghcr.io/unict-cclab/mentat:v2.3.4")
	assertLogContains(t, files.logs, "mentat-config.log", "SLEEP_SECONDS=7")
}

func fakeCommand(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertLogContains(t *testing.T, dir, name string, expected ...string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	content := string(data)
	for _, value := range expected {
		if !strings.Contains(content, value) {
			t.Fatalf("%s does not contain %q:\n%s", name, value, content)
		}
	}
}
