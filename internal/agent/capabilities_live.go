package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

type liveCapabilitiesPayload struct {
	Capabilities []string          `json:"capabilities"`
	AgentTools   map[string]string `json:"agent_tools"`
}

func liveCapabilitiesLines(ctx context.Context, project projects.Manifest) []string {
	if strings.TrimSpace(project.CapabilitiesEndpoint.URL) == "" {
		return []string{"  live: skipped, endpoint not configured"}
	}
	payload, err := fetchLiveCapabilities(ctx, project.CapabilitiesEndpoint.URL)
	if err != nil {
		return []string{"  live: unavailable: " + err.Error()}
	}

	lines := []string{"  live: ok capabilities=" + strings.Join(sortedCopy(payload.Capabilities), ", ")}
	lines = append(lines, "  live_diff: "+formatDiff(project.Capabilities, payload.Capabilities))
	if len(project.AgentTools) > 0 || len(payload.AgentTools) > 0 {
		lines = append(lines, "  live_agent_tools_diff: "+formatDiff(mapKeys(project.AgentTools), mapKeys(payload.AgentTools)))
	}
	return lines
}

func fetchLiveCapabilities(ctx context.Context, endpoint string) (liveCapabilitiesPayload, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return liveCapabilitiesPayload{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return liveCapabilitiesPayload{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return liveCapabilitiesPayload{}, fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return liveCapabilitiesPayload{}, err
	}
	var payload liveCapabilitiesPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return liveCapabilitiesPayload{}, err
	}
	return payload, nil
}

func formatDiff(manifestValues []string, liveValues []string) string {
	missingInLive := difference(manifestValues, liveValues)
	missingInManifest := difference(liveValues, manifestValues)
	if len(missingInLive) == 0 && len(missingInManifest) == 0 {
		return "manifest/live consistent"
	}
	return fmt.Sprintf("missing_in_live=%s missing_in_manifest=%s",
		strings.Join(missingInLive, ","),
		strings.Join(missingInManifest, ","),
	)
}

func difference(left []string, right []string) []string {
	rightSet := map[string]struct{}{}
	for _, value := range right {
		value = strings.TrimSpace(value)
		if value != "" {
			rightSet[value] = struct{}{}
		}
	}
	var missing []string
	for _, value := range left {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := rightSet[value]; !ok {
			missing = append(missing, value)
		}
	}
	sort.Strings(missing)
	return missing
}

func mapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedCopy(values []string) []string {
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	return copied
}
