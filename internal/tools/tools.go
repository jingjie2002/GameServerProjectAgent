package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jingjie2002/GameServerProjectAgent/internal/audit"
	"github.com/jingjie2002/GameServerProjectAgent/internal/permissions"
	"github.com/jingjie2002/GameServerProjectAgent/internal/projects"
)

var ErrPermissionDenied = errors.New("permission denied")

type Runner struct {
	Audit *audit.Store
}

type Result struct {
	ProjectID string
	Command   string
	Status    string
	Output    string
	Duration  time.Duration
}

func (r Runner) RunProjectCommand(ctx context.Context, mode permissions.Mode, manifest projects.Manifest, commandName string) (Result, error) {
	command, ok := manifest.Commands[commandName]
	if !ok {
		return Result{}, fmt.Errorf("project %s has no command %q", manifest.ID, commandName)
	}
	if !mode.Allows(command.Mode) {
		_ = r.append(mode, "run_"+commandName, manifest.ID, "blocked", command.Mode.String())
		return Result{}, fmt.Errorf("%w: command %s requires %s, current mode is %s", ErrPermissionDenied, commandName, command.Mode, mode)
	}
	args := strings.Fields(command.Command)
	if len(args) == 0 {
		return Result{}, fmt.Errorf("empty command %q", commandName)
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = manifest.Root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(manifest.Root, ".gocache"))
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	status := "ok"
	if err != nil {
		status = "failed"
	}
	_ = r.append(mode, "run_"+commandName, manifest.ID, status, command.Command)
	return Result{
		ProjectID: manifest.ID,
		Command:   command.Command,
		Status:    status,
		Output:    output.String(),
		Duration:  time.Since(start),
	}, err
}

func (r Runner) append(mode permissions.Mode, action string, projectID string, status string, detail string) error {
	if r.Audit == nil {
		return nil
	}
	return r.Audit.Append(audit.Event{
		Mode:      mode.String(),
		Action:    action,
		ProjectID: projectID,
		Status:    status,
		Detail:    detail,
	})
}
