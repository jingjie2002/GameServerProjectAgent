package generated

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
	"github.com/jingjie2002/GameServerProjectAgent/internal/registry"
)

type Options struct {
	Path       string
	Home       string
	Workspace  string
	ConfigPath string
	Confirm    bool
}

type Result struct {
	Manifest      projects.Manifest
	ManifestPath  string
	ConfigPath    string
	Confirmed     bool
	Registered    bool
	AlreadyExists bool
}

func Register(opts Options) (Result, error) {
	manifestPath, err := ResolveManifestPath(opts.Path)
	if err != nil {
		return Result{}, err
	}
	manifest, err := projects.LoadManifest(manifestPath)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Manifest:     manifest,
		ManifestPath: manifestPath,
		ConfigPath:   opts.ConfigPath,
		Confirmed:    opts.Confirm,
	}
	if !opts.Confirm {
		return result, nil
	}

	registered, err := registry.RegisterManifest(registry.Options{
		Home:       opts.Home,
		Workspace:  opts.Workspace,
		ConfigPath: opts.ConfigPath,
	}, manifestPath)
	if err != nil {
		return result, err
	}
	result.Registered = registered.Registered
	result.AlreadyExists = registered.AlreadyExists
	return result, nil
}

func ResolveManifestPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	clean := filepath.Clean(path)
	info, err := os.Stat(clean)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return filepath.Join(clean, "agent.generated.yaml"), nil
	}
	return clean, nil
}

func FormatResult(result Result) string {
	title := "generated manifest 注册预览"
	if result.Confirmed {
		title = "generated manifest 注册完成"
	}
	lines := []string{
		title,
		"manifest: " + result.ManifestPath,
		"project_id: " + result.Manifest.ID,
		"project_name: " + result.Manifest.Name,
		"root: " + result.Manifest.Root,
		"config: " + result.ConfigPath,
	}
	if result.Manifest.Health.URL != "" {
		lines = append(lines, "health: "+result.Manifest.Health.URL)
	}
	if result.Manifest.CapabilitiesEndpoint.URL != "" {
		lines = append(lines, "capabilities: "+result.Manifest.CapabilitiesEndpoint.URL)
	}
	if !result.Confirmed {
		lines = append(lines, "confirmed: false")
		lines = append(lines, "registered: no")
		lines = append(lines, "status: preview_only")
		lines = append(lines, "确认注册后再执行：gsa register-generated "+quoteIfNeeded(result.ManifestPath)+" --confirm")
		return strings.Join(lines, "\n")
	}
	lines = append(lines, "confirmed: true")
	if result.Registered {
		lines = append(lines, "registered: yes")
		lines = append(lines, "status: registered")
	} else if result.AlreadyExists {
		lines = append(lines, "registered: no")
		lines = append(lines, "status: already_exists")
	} else {
		lines = append(lines, "registered: no")
		lines = append(lines, "status: unchanged")
	}
	return strings.Join(lines, "\n")
}

func quoteIfNeeded(path string) string {
	if strings.ContainsAny(path, " \t") {
		return fmt.Sprintf("%q", path)
	}
	return path
}
