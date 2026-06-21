package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/unict-cclab/experiment-executor/internal/config"
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
				SchedulerName: "default-scheduler", ProxyNodes: "all",
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
