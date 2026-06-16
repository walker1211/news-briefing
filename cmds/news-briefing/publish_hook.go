package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/walker1211/news-briefing/internal/config"
)

type publishHookRequest struct {
	MarkdownFile     string
	CardManifestFile string
	SourceApp        string
	Date             string
	Period           string
}

func runPublishHook(ctx context.Context, cfg config.PublishHookConfig, req publishHookRequest) error {
	if !cfg.Enabled {
		return nil
	}
	command := config.ExpandHomePath(strings.TrimSpace(cfg.Command))
	if command == "" {
		return fmt.Errorf("publish_hook.command is empty")
	}
	args := expandPublishHookArgs(cfg.Args, req)
	cmd := exec.CommandContext(ctx, command, args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("publish hook failed: %s", describePublishHookRunError(ctx, err))
	}
	return nil
}

func describePublishHookRunError(ctx context.Context, err error) string {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr.Error()
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return fmt.Sprintf("exit code %d", code)
		}
		return "command terminated"
	}

	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return "command unavailable"
	}

	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return "command unavailable"
	}

	return "command failed"
}

func expandPublishHookArgs(args []string, req publishHookRequest) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.ReplaceAll(arg, "{markdown_file}", req.MarkdownFile)
		arg = strings.ReplaceAll(arg, "{card_manifest}", req.CardManifestFile)
		arg = strings.ReplaceAll(arg, "{xhs_card_manifest}", req.CardManifestFile)
		arg = strings.ReplaceAll(arg, "{source_app}", req.SourceApp)
		arg = strings.ReplaceAll(arg, "{date}", req.Date)
		arg = strings.ReplaceAll(arg, "{period}", req.Period)
		out = append(out, arg)
	}
	return out
}
