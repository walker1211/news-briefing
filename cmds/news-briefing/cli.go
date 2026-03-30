package main

import (
	"fmt"
	"strings"
)

type command interface{ isCommand() }

type runCommand struct {
	raw     bool
	noEmail bool
}

type regenCommand struct {
	fromRaw    string
	toRaw      string
	period     string
	ignoreSeen bool
	sendEmail  bool
	raw        bool
}

type fetchCommand struct{ zh bool }
type serveCommand struct{}
type deepCommand struct {
	topic      string
	fromRaw    string
	toRaw      string
	ignoreSeen bool
	sendEmail  bool
}
type helpCommand struct{}

func (runCommand) isCommand()   {}
func (regenCommand) isCommand() {}
func (fetchCommand) isCommand() {}
func (serveCommand) isCommand() {}
func (deepCommand) isCommand()  {}
func (helpCommand) isCommand()  {}

func parseArgs(args []string) (command, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing command")
	}

	cmdName := args[0]
	normalizedCmdName := normalizeCommandName(cmdName)
	if !isKnownCommandName(normalizedCmdName) {
		return nil, fmt.Errorf("unknown command: %s", args[0])
	}
	if err := preValidateCommandArgs(normalizedCmdName, args[1:]); err != nil {
		return nil, err
	}

	switch normalizedCmdName {
	case "run":
		return runCommand{raw: hasFlagIn(args[1:], "--raw"), noEmail: hasFlagIn(args[1:], "--no-email")}, nil
	case "regen":
		fromRaw, ok := readStringFlag(args[1:], "--from")
		if !ok {
			return nil, fmt.Errorf("--from is required")
		}
		toRaw, ok := readStringFlag(args[1:], "--to")
		if !ok {
			return nil, fmt.Errorf("--to is required")
		}
		period, ok := readStringFlag(args[1:], "--period")
		if !ok || period == "" {
			if rawPeriod, exists := nextTokenAfterFlag(args[1:], "--period"); exists && strings.HasPrefix(rawPeriod, "-") {
				period = rawPeriod
			} else {
				period = defaultPeriodFromRaw(toRaw)
			}
		}
		if err := validatePeriod(period); err != nil {
			return nil, err
		}
		return regenCommand{fromRaw: fromRaw, toRaw: toRaw, period: period, ignoreSeen: hasFlagIn(args[1:], "--ignore-seen"), sendEmail: hasFlagIn(args[1:], "--send-email"), raw: hasFlagIn(args[1:], "--raw")}, nil
	case "fetch":
		return fetchCommand{zh: hasFlagIn(args[1:], "--zh")}, nil
	case "serve":
		return serveCommand{}, nil
	case "deep":
		fromRaw, fromSet := readStringFlag(args[1:], "--from")
		toRaw, toSet := readStringFlag(args[1:], "--to")
		if fromSet != toSet {
			return nil, fmt.Errorf("--from and --to must be provided together")
		}
		topic := collectDeepTopicArgs(args[1:])
		if topic == "" {
			return nil, fmt.Errorf("missing deep topic")
		}
		return deepCommand{topic: topic, fromRaw: fromRaw, toRaw: toRaw, ignoreSeen: hasFlagIn(args[1:], "--ignore-seen"), sendEmail: hasFlagIn(args[1:], "--send-email")}, nil
	case "help":
		return helpCommand{}, nil
	default:
		return nil, fmt.Errorf("unknown command: %s", args[0])
	}
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
	case "run", "regen", "fetch", "serve", "deep", "help":
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
		return map[string]struct{}{"--raw": {}, "--no-email": {}}, nil, false
	case "fetch":
		return map[string]struct{}{"--zh": {}}, nil, false
	case "serve", "help":
		return nil, nil, false
	case "deep":
		return map[string]struct{}{"--ignore-seen": {}, "--send-email": {}}, map[string]struct{}{"--from": {}, "--to": {}}, true
	case "regen":
		return map[string]struct{}{"--ignore-seen": {}, "--send-email": {}, "--raw": {}}, map[string]struct{}{"--from": {}, "--to": {}, "--period": {}}, false
	default:
		return nil, nil, false
	}
}
