package projects

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
)

type Manifest struct {
	ID                   string
	Name                 string
	Description          string
	Type                 string
	Root                 string
	ManifestPath         string
	Health               Endpoint
	Metrics              Endpoint
	CapabilitiesEndpoint Endpoint
	Commands             map[string]Command
	Docs                 []string
	Capabilities         []string
	Forbidden            []string
	AgentTools           map[string]string
}

type Endpoint struct {
	URL       string
	LegacyURL string
	Format    string
}

type Command struct {
	Name    string
	Command string
	Mode    permissions.Mode
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, err
	}
	manifest.ManifestPath = filepath.Clean(path)
	return manifest, nil
}

func LoadManifests(paths []string) ([]Manifest, error) {
	manifests := make([]Manifest, 0, len(paths))
	for _, path := range paths {
		manifest, err := LoadManifest(path)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}
		manifests = append(manifests, manifest)
	}
	return manifests, nil
}

func ParseManifest(data []byte) (Manifest, error) {
	manifest := Manifest{
		Commands:   map[string]Command{},
		AgentTools: map[string]string{},
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var section string
	var commandName string
	for scanner.Scan() {
		raw := strings.TrimPrefix(stripComment(scanner.Text()), "\ufeff")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		indent := leadingSpaces(raw)
		line := strings.TrimSpace(raw)
		key, value, hasKV := splitKV(line)
		if indent == 0 && hasKV {
			section = key
			commandName = ""
			switch key {
			case "id":
				manifest.ID = value
			case "name":
				manifest.Name = value
			case "description":
				manifest.Description = value
			case "type":
				manifest.Type = value
			case "root":
				manifest.Root = filepath.FromSlash(value)
			}
			continue
		}
		switch section {
		case "health":
			assignEndpoint(&manifest.Health, key, value, hasKV)
		case "metrics":
			assignEndpoint(&manifest.Metrics, key, value, hasKV)
		case "capabilities_endpoint":
			assignEndpoint(&manifest.CapabilitiesEndpoint, key, value, hasKV)
		case "commands":
			if indent == 2 && strings.HasSuffix(line, ":") {
				commandName = strings.TrimSuffix(line, ":")
				manifest.Commands[commandName] = Command{Name: commandName, Mode: permissions.AutoReviewMode}
				continue
			}
			if indent >= 4 && commandName != "" && hasKV {
				command := manifest.Commands[commandName]
				switch key {
				case "command":
					command.Command = value
				case "mode":
					mode, err := permissions.ParseMode(value)
					if err != nil {
						return Manifest{}, err
					}
					command.Mode = mode
				}
				manifest.Commands[commandName] = command
			}
		case "docs":
			if item, ok := listItem(line); ok {
				manifest.Docs = append(manifest.Docs, item)
			}
		case "capabilities":
			if item, ok := listItem(line); ok {
				manifest.Capabilities = append(manifest.Capabilities, item)
			}
		case "forbidden":
			if item, ok := listItem(line); ok {
				manifest.Forbidden = append(manifest.Forbidden, item)
			}
		case "agent_tools":
			if indent >= 2 && hasKV {
				manifest.AgentTools[key] = value
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Manifest{}, err
	}
	if manifest.ID == "" || manifest.Name == "" || manifest.Root == "" {
		return Manifest{}, fmt.Errorf("manifest must include id, name and root")
	}
	return manifest, nil
}

func FindDefaultManifestPaths(workspace string) []string {
	return []string{
		filepath.Join(workspace, "CoreRank", "agent.yaml"),
		filepath.Join(workspace, "ArenaGate", "agent.yaml"),
		filepath.Join(workspace, "GameOps", "agent.yaml"),
	}
}

func FindWorkspace(cwd string) string {
	if env := os.Getenv("GSA_WORKSPACE"); env != "" {
		return filepath.Clean(env)
	}

	clean := filepath.Clean(cwd)
	for {
		if fileExists(filepath.Join(clean, "CoreRank", "agent.yaml")) &&
			fileExists(filepath.Join(clean, "ArenaGate", "agent.yaml")) &&
			fileExists(filepath.Join(clean, "GameOps", "agent.yaml")) {
			return clean
		}
		parent := filepath.Dir(clean)
		if parent == clean {
			break
		}
		clean = parent
	}
	return cwd
}

func assignEndpoint(endpoint *Endpoint, key string, value string, ok bool) {
	if !ok {
		return
	}
	switch key {
	case "url":
		endpoint.URL = value
	case "legacy_url":
		endpoint.LegacyURL = value
	case "format":
		endpoint.Format = value
	}
}

func splitKV(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	value := strings.TrimSpace(line[idx+1:])
	value = strings.Trim(value, `"'`)
	return key, value, true
}

func listItem(line string) (string, bool) {
	if !strings.HasPrefix(line, "- ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "- ")), true
}

func stripComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
