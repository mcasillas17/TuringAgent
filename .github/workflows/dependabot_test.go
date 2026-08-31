package workflows_test

import (
	"fmt"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type dependabotConfig struct {
	Version int `yaml:"version"`
	Updates []struct {
		PackageEcosystem string   `yaml:"package-ecosystem"`
		Directory        string   `yaml:"directory"`
		Directories      []string `yaml:"directories"`
		Schedule         struct {
			Interval string `yaml:"interval"`
		} `yaml:"schedule"`
	} `yaml:"updates"`
}

func TestDependabotCoversEveryDependencySource(t *testing.T) {
	data, err := os.ReadFile("../dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}

	var config dependabotConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != 2 {
		t.Fatalf("version = %d, want 2", config.Version)
	}

	want := map[string]bool{
		"gomod:/":                                 false,
		"gomod:/turing-backend/mcp-files":         false,
		"gomod:/turing-backend/mcp-system":        false,
		"npm:/turing-backend":                     false,
		"pub:/turing-client/turing_app":           false,
		"docker:/turing-backend/agent-runtime-go": false,
		"docker:/turing-backend/mcp-files":        false,
		"docker:/turing-backend/mcp-system":       false,
		"docker:/turing-backend/orchestrator-go":  false,
		"github-actions:/":                        false,
	}

	for _, update := range config.Updates {
		if update.Schedule.Interval != "weekly" {
			t.Errorf("%s schedule interval = %q, want weekly", update.PackageEcosystem, update.Schedule.Interval)
		}
		directories := update.Directories
		if update.Directory != "" {
			directories = append(directories, update.Directory)
		}
		for _, directory := range directories {
			key := fmt.Sprintf("%s:%s", update.PackageEcosystem, directory)
			if _, ok := want[key]; !ok {
				t.Errorf("unexpected dependency source %q", key)
				continue
			}
			want[key] = true
		}
	}

	for source, found := range want {
		if !found {
			t.Errorf("missing dependency source %q", source)
		}
	}
}
