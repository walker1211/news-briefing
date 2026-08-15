package sourcehealth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/walker1211/news-briefing/internal/statefile"
)

const schemaVersion = 1

type Issue struct {
	Key  string
	Name string
}

type Result struct {
	VisibleKeys []string
	Recoveries  []string
}

type state struct {
	SchemaVersion int              `json:"schema_version"`
	Sources       map[string]entry `json:"sources"`
}

type entry struct {
	Name                string    `json:"name"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	Alerted             bool      `json:"alerted"`
	LastWindow          string    `json:"last_window"`
	LastFailureAt       time.Time `json:"last_failure_at"`
}

func Update(path, window string, now time.Time, alertAfter int, issues []Issue) (Result, error) {
	if alertAfter < 1 {
		alertAfter = 1
	}
	current, err := load(path)
	if err != nil {
		return Result{}, err
	}
	active := make(map[string]Issue, len(issues))
	for _, issue := range issues {
		if issue.Key == "" || issue.Name == "" {
			continue
		}
		active[issue.Key] = issue
	}

	result := Result{}
	for key, issue := range active {
		item := current.Sources[key]
		if item.LastWindow != window {
			item.ConsecutiveFailures++
		}
		item.Name = issue.Name
		item.LastWindow = window
		item.LastFailureAt = now
		if item.ConsecutiveFailures >= alertAfter {
			item.Alerted = true
			result.VisibleKeys = append(result.VisibleKeys, key)
		}
		current.Sources[key] = item
	}

	for key, item := range current.Sources {
		if _, failed := active[key]; failed {
			continue
		}
		if item.Alerted {
			result.Recoveries = append(result.Recoveries, item.Name)
		}
		delete(current.Sources, key)
	}
	sort.Strings(result.VisibleKeys)
	sort.Strings(result.Recoveries)
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return Result{}, fmt.Errorf("encode source health state: %w", err)
	}
	if err := statefile.WriteAtomic(path, append(data, '\n'), 0o600); err != nil {
		return Result{}, fmt.Errorf("write source health state: %w", err)
	}
	return result, nil
}

func load(path string) (state, error) {
	current := state{SchemaVersion: schemaVersion, Sources: map[string]entry{}}
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, os.ErrNotExist) {
		return current, nil
	}
	if err != nil {
		return state{}, fmt.Errorf("read source health state: %w", err)
	}
	if err := json.Unmarshal(data, &current); err != nil {
		return state{}, fmt.Errorf("decode source health state: %w", err)
	}
	if current.SchemaVersion != schemaVersion {
		return state{}, fmt.Errorf("unsupported source health schema version %d", current.SchemaVersion)
	}
	if current.Sources == nil {
		current.Sources = map[string]entry{}
	}
	return current, nil
}
