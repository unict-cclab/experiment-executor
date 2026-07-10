package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSuite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.tmpl"), []byte("kind: List\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	experiment := `name: test
runs: 1
lifecycle: {cluster: existing}
tools:
  proxmoxK3s: {config: {clusters: []}}
  application: {name: app, template: app.tmpl, proxyNodes: none}
  loadGen: {config: {locustfile: locustfile.py, host: http://example.test, pattern: {type: constant, rps: 1, duration: 1s}}}
`
	for _, name := range []string{"a", "b"} {
		path := filepath.Join(dir, name+".yaml")
		if err := os.WriteFile(path, []byte(experiment), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	suitePath := filepath.Join(dir, "suite.yaml")
	data := []byte("name: comparison\noutputDir: results\nexperiments:\n  - name: baseline\n    config: a.yaml\n  - name: candidate\n    config: b.yaml\n")
	if err := os.WriteFile(suitePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	suite, entries, err := LoadSuite(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name != "baseline" || entries[1].Name != "candidate" {
		t.Fatalf("entries = %#v", entries)
	}
	if want := filepath.Join(dir, "results"); suite.ResolvedOutputDir() != want {
		t.Fatalf("output = %q, want %q", suite.ResolvedOutputDir(), want)
	}
}
