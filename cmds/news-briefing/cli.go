package main

import (
	"fmt"
	"strconv"
	"strings"
)

type command interface{ isCommand() }

type runCommand struct {
	raw       bool
	noEmail   bool
	noPublish bool
}

type regenCommand struct {
	fromRaw               string
	toRaw                 string
	period                string
	ignoreSeen            bool
	sendEmail             bool
	raw                   bool
	noPublish             bool
	xVisibleHistoryDays   int
	xVisibleHistoryDir    string
	maxArticlesByCategory map[string]int
}

type fetchCommand struct{ zh bool }
type alertsCommand struct{}
type xRoutesCommand struct{}
type xReadyCommand struct {
	fromRaw   string
	toRaw     string
	period    string
	noPublish bool
}
type serveCommand struct{ noPublish bool }
type deepCommand struct {
	topic      string
	fromRaw    string
	toRaw      string
	ignoreSeen bool
	sendEmail  bool
}
type resendMDCommand struct{ file string }
type helpCommand struct{}

func (runCommand) isCommand()      {}
func (regenCommand) isCommand()    {}
func (fetchCommand) isCommand()    {}
func (alertsCommand) isCommand()   {}
func (xRoutesCommand) isCommand()  {}
func (xReadyCommand) isCommand()   {}
func (serveCommand) isCommand()    {}
func (deepCommand) isCommand()     {}
func (resendMDCommand) isCommand() {}
func (helpCommand) isCommand()     {}

func parseArgs(args []string) (command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing command")
	}

	cmdName := args[0]
	normalizedCmdName := normalizeCommandName(cmdName)
	if !isKnownCommandName(normalizedCmdName) {
		return nil, fmt.Errorf("unknown command: %s", args[0])
	}
	if normalizedCmdName == "x" {
		return parseXCommand(args[1:])
	}
	cmdArgs := args[1:]
	if err := preValidateCommandArgs(normalizedCmdName, cmdArgs); err != nil {
		return nil, err
	}

	switch normalizedCmdName {
	case "run":
		return parseRunCommand(cmdArgs)
	case "regen":
		return parseRegenCommand(cmdArgs)
	case "fetch":
		return parseFetchCommand(cmdArgs)
	case "alerts":
		return alertsCommand{}, nil
	case "serve":
		return serveCommand{noPublish: hasFlagIn(cmdArgs, "--no-publish")}, nil
	case "deep":
		return parseDeepCommand(cmdArgs)
	case "resend-md":
		return parseResendMDCommand(cmdArgs)
	case "help":
		return helpCommand{}, nil
	default:
		return nil, fmt.Errorf("unknown command: %s", args[0])
	}
}

func parseRunCommand(args []string) (command, error) {
	return runCommand{raw: hasFlagIn(args, "--raw"), noEmail: hasFlagIn(args, "--no-email"), noPublish: hasFlagIn(args, "--no-publish")}, nil
}

func parseRegenCommand(args []string) (command, error) {
	fromRaw, ok := readStringFlag(args, "--from")
	if !ok {
		return nil, fmt.Errorf("--from is required")
	}
	toRaw, ok := readStringFlag(args, "--to")
	if !ok {
		return nil, fmt.Errorf("--to is required")
	}
	period, ok := readStringFlag(args, "--period")
	if !ok || period == "" {
		if rawPeriod, exists := nextTokenAfterFlag(args, "--period"); exists && strings.HasPrefix(rawPeriod, "-") {
			period = rawPeriod
		} else {
			period = defaultPeriodFromRaw(toRaw)
		}
	}
	if err := validatePeriod(period); err != nil {
		return nil, err
	}
	historyDays := 0
	if rawHistoryDays, ok := readStringFlag(args, "--x-visible-history-days"); ok {
		parsed, err := parsePositiveIntFlag(rawHistoryDays, "--x-visible-history-days")
		if err != nil {
			return nil, err
		}
		historyDays = parsed
	}
	historyDir, _ := readStringFlag(args, "--x-visible-history-dir")
	maxArticlesByCategory := map[string]int(nil)
	if rawMaxArticles, ok := readStringFlag(args, "--max-articles"); ok {
		parsed, err := parseCategoryArticleLimits(rawMaxArticles)
		if err != nil {
			return nil, err
		}
		maxArticlesByCategory = parsed
	}
	return regenCommand{fromRaw: fromRaw, toRaw: toRaw, period: period, ignoreSeen: hasFlagIn(args, "--ignore-seen"), sendEmail: hasFlagIn(args, "--send-email"), raw: hasFlagIn(args, "--raw"), noPublish: hasFlagIn(args, "--no-publish"), xVisibleHistoryDays: historyDays, xVisibleHistoryDir: historyDir, maxArticlesByCategory: maxArticlesByCategory}, nil
}

func parseFetchCommand(args []string) (command, error) {
	return fetchCommand{zh: hasFlagIn(args, "--zh")}, nil
}

func parseDeepCommand(args []string) (command, error) {
	fromRaw, fromSet := readStringFlag(args, "--from")
	toRaw, toSet := readStringFlag(args, "--to")
	if fromSet != toSet {
		return nil, fmt.Errorf("--from and --to must be provided together")
	}
	topic := collectDeepTopicArgs(args)
	if topic == "" {
		return nil, fmt.Errorf("missing deep topic")
	}
	return deepCommand{topic: topic, fromRaw: fromRaw, toRaw: toRaw, ignoreSeen: hasFlagIn(args, "--ignore-seen"), sendEmail: hasFlagIn(args, "--send-email")}, nil
}

func parseResendMDCommand(args []string) (command, error) {
	file, ok := readStringFlag(args, "--file")
	if !ok || file == "" {
		return nil, fmt.Errorf("--file is required")
	}
	return resendMDCommand{file: file}, nil
}

func parseXCommand(args []string) (command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing x subcommand")
	}
	switch args[0] {
	case "routes":
		if len(args) > 1 {
			return nil, fmt.Errorf("unexpected arguments for x routes: %s", strings.Join(args[1:], " "))
		}
		return xRoutesCommand{}, nil
	case "ready":
		return parseXReadyCommand(args[1:])
	default:
		return nil, fmt.Errorf("unsupported x subcommand: %s", args[0])
	}
}

func parseXReadyCommand(args []string) (command, error) {
	cmd := xReadyCommand{}
	for i := 0; i < len(args); i++ {
		token := args[i]
		switch token {
		case "--no-publish":
			cmd.noPublish = true
		case "--from", "--to", "--period":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, fmt.Errorf("%s is required", token)
			}
			value := args[i+1]
			i++
			switch token {
			case "--from":
				cmd.fromRaw = value
			case "--to":
				cmd.toRaw = value
			case "--period":
				cmd.period = value
			}
		default:
			if strings.HasPrefix(token, "-") {
				return nil, fmt.Errorf("unknown flag for x ready: %s", token)
			}
			return nil, fmt.Errorf("unexpected arguments for x ready: %s", strings.Join(args[i:], " "))
		}
	}
	if strings.TrimSpace(cmd.fromRaw) == "" {
		return nil, fmt.Errorf("--from is required")
	}
	if strings.TrimSpace(cmd.toRaw) == "" {
		return nil, fmt.Errorf("--to is required")
	}
	if cmd.period != "" {
		if err := validatePeriod(cmd.period); err != nil {
			return nil, err
		}
	}
	return cmd, nil
}

func hasFlagIn(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}
	return false
}

func readStringFlag(args []string, flag string) (string, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			if strings.HasPrefix(args[i+1], "--") {
				return "", false
			}
			return args[i+1], true
		}
	}
	return "", false
}

func nextTokenAfterFlag(args []string, flag string) (string, bool) {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1], true
		}
	}
	return "", false
}

func parsePositiveIntFlag(value string, flag string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", flag)
	}
	return parsed, nil
}

func parseCategoryArticleLimits(value string) (map[string]int, error) {
	limits := make(map[string]int)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		category, rawLimit, ok := strings.Cut(item, "=")
		category = strings.TrimSpace(category)
		if !ok || category == "" {
			return nil, fmt.Errorf("--max-articles item %q must be CATEGORY=N", item)
		}
		limit, err := parsePositiveIntFlag(rawLimit, "--max-articles")
		if err != nil {
			return nil, err
		}
		limits[category] = limit
	}
	if len(limits) == 0 {
		return nil, fmt.Errorf("--max-articles must include at least one CATEGORY=N item")
	}
	return limits, nil
}

func collectDeepTopicArgs(args []string) string {
	var parts []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			switch arg {
			case "--from", "--to":
				if i+1 < len(args) {
					i++
				}
			}
			continue
		}
		parts = append(parts, arg)
	}
	return strings.Join(parts, " ")
}

func normalizeCommandName(name string) string {
	switch name {
	case "-h", "--help":
		return "help"
	default:
		return name
	}
}

func isKnownCommandName(name string) bool {
	switch name {
	case "run", "regen", "fetch", "alerts", "x", "serve", "deep", "resend-md", "help":
		return true
	default:
		return false
	}
}

func preValidateCommandArgs(cmd string, args []string) error {
	allowedBoolFlags, allowedValueFlags, allowPositional := commandValidationRules(cmd)

	for i := 0; i < len(args); i++ {
		token := args[i]

		if strings.HasPrefix(token, "-") {
			if _, ok := allowedBoolFlags[token]; ok {
				continue
			}
			if _, ok := allowedValueFlags[token]; ok {
				if i+1 < len(args) {
					i++
				}
				continue
			}
			return fmt.Errorf("unknown flag for %s: %s", cmd, token)
		}

		if !allowPositional {
			return fmt.Errorf("unexpected arguments for %s: %s", cmd, strings.Join(args[i:], " "))
		}
	}

	return nil
}

func commandValidationRules(cmd string) (map[string]struct{}, map[string]struct{}, bool) {
	switch cmd {
	case "run":
		return map[string]struct{}{"--raw": {}, "--no-email": {}, "--no-publish": {}}, nil, false
	case "fetch":
		return map[string]struct{}{"--zh": {}}, nil, false
	case "serve":
		return map[string]struct{}{"--no-publish": {}}, nil, false
	case "alerts", "help":
		return nil, nil, false
	case "deep":
		return map[string]struct{}{"--ignore-seen": {}, "--send-email": {}}, map[string]struct{}{"--from": {}, "--to": {}}, true
	case "resend-md":
		return nil, map[string]struct{}{"--file": {}}, false
	case "regen":
		return map[string]struct{}{"--ignore-seen": {}, "--send-email": {}, "--raw": {}, "--no-publish": {}}, map[string]struct{}{"--from": {}, "--to": {}, "--period": {}, "--x-visible-history-days": {}, "--x-visible-history-dir": {}, "--max-articles": {}}, false
	default:
		return nil, nil, false
	}
}
