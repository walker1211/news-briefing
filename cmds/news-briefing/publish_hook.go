package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/walker1211/news-briefing/internal/config"
)

type publishHookRequest struct {
	MarkdownFile string
	SourceApp    string
	Date         string
	Period       string
}

func runPublishHook(ctx context.Context, cfg config.PublishHookConfig, req publishHookRequest) error {
	if !cfg.Enabled {
		return nil
	}
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		return fmt.Errorf("publish_hook.command is empty")
	}
	args := expandPublishHookArgs(cfg.Args, req)
	cmd := exec.CommandContext(ctx, command, args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("publish hook failed: %w", err)
	}
	return nil
}

func expandPublishHookArgs(args []string, req publishHookRequest) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.ReplaceAll(arg, "{markdown_file}", req.MarkdownFile)
		arg = strings.ReplaceAll(arg, "{source_app}", req.SourceApp)
		arg = strings.ReplaceAll(arg, "{date}", req.Date)
		arg = strings.ReplaceAll(arg, "{period}", req.Period)
		out = append(out, arg)
	}
	return out
}
