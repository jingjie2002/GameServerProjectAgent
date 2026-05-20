package registry

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
	"github.com/jingjie2002/GameServerProjectAgent/internal/setup"
)

type Options struct {
	Home       string
	Workspace  string
	ConfigPath string
}

type Result struct {
	Manifest      projects.Manifest
	ManifestPath  string
	ConfigPath    string
	Registered    bool
	AlreadyExists bool
}

func RegisterManifest(opts Options, manifestPath string) (Result, error) {
	clean := filepath.Clean(manifestPath)
	manifest, err := projects.LoadManifest(clean)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Manifest:     manifest,
		ManifestPath: clean,
		ConfigPath:   opts.ConfigPath,
	}

	cfg, err := setup.LoadConfig(opts.ConfigPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return result, err
		}
		cfg = setup.Config{
			Version:   1,
			Home:      opts.Home,
			Workspace: opts.Workspace,
			LLM: setup.LLMConfig{
				Provider:  "none",
				APIKeyEnv: "GSA_LLM_API_KEY",
			},
		}
	}
	for _, existing := range cfg.ProjectManifestPaths() {
		if samePath(existing, clean) {
			result.AlreadyExists = true
			return result, nil
		}
	}

	cfg.Projects = append(cfg.Projects, setup.ProjectConfig{ManifestPath: clean})
	if cfg.Home == "" {
		cfg.Home = opts.Home
	}
	if cfg.Workspace == "" {
		cfg.Workspace = opts.Workspace
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.LLM.APIKeyEnv == "" {
		cfg.LLM.APIKeyEnv = "GSA_LLM_API_KEY"
	}
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "none"
	}
	if err := setup.WriteConfig(opts.ConfigPath, cfg); err != nil {
		return result, err
	}
	result.Registered = true
	return result, nil
}

func samePath(a string, b string) bool {
	aa, errA := filepath.Abs(filepath.Clean(a))
	bb, errB := filepath.Abs(filepath.Clean(b))
	if errA == nil && errB == nil {
		return strings.EqualFold(aa, bb)
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
