package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Suite struct {
	Name        string       `yaml:"name" json:"name"`
	OutputDir   string       `yaml:"outputDir,omitempty" json:"outputDir,omitempty"`
	Experiments []SuiteEntry `yaml:"experiments" json:"experiments"`
	SourceDir   string       `yaml:"-" json:"-"`
}

type SuiteEntry struct {
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
	Config string `yaml:"config" json:"config"`
}

type LoadedSuiteEntry struct {
	Name       string
	ConfigPath string
	Experiment *Experiment
}

func LoadSuite(path string) (*Suite, []LoadedSuiteEntry, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolving suite path: %w", err)
	}
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading suite: %w", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var suite Suite
	if err := decoder.Decode(&suite); err != nil {
		return nil, nil, fmt.Errorf("decoding suite: %w", err)
	}
	suite.SourceDir = filepath.Dir(absolutePath)
	if !regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`).MatchString(suite.Name) {
		return nil, nil, fmt.Errorf("suite name must be a lowercase DNS-style name")
	}
	if len(suite.Experiments) < 2 {
		return nil, nil, fmt.Errorf("suite must contain at least two experiments")
	}
	loaded := make([]LoadedSuiteEntry, 0, len(suite.Experiments))
	seen := make(map[string]bool)
	for i, entry := range suite.Experiments {
		if entry.Config == "" {
			return nil, nil, fmt.Errorf("experiments[%d].config is required", i)
		}
		configPath := entry.Config
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(suite.SourceDir, configPath)
		}
		experiment, err := Load(configPath)
		if err != nil {
			return nil, nil, fmt.Errorf("loading experiments[%d]: %w", i, err)
		}
		name := entry.Name
		if name == "" {
			name = experiment.Name
		}
		if !validName.MatchString(name) {
			return nil, nil, fmt.Errorf("experiments[%d].name must be a lowercase DNS-style name", i)
		}
		if seen[name] {
			return nil, nil, fmt.Errorf("duplicate suite experiment name %q", name)
		}
		seen[name] = true
		loaded = append(loaded, LoadedSuiteEntry{Name: name, ConfigPath: configPath, Experiment: experiment})
	}
	return &suite, loaded, nil
}

func (s Suite) ResolvedOutputDir() string {
	path := s.OutputDir
	if path == "" {
		path = filepath.Join("suite-results", s.Name)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.SourceDir, path)
	}
	return filepath.Clean(path)
}
