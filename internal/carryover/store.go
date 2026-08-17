package carryover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
	"github.com/walker1211/news-briefing/internal/statefile"
)

const (
	SchemaVersion       = 1
	StatusPending       = "pending"
	StatusConsumed      = "consumed"
	StatusExpired       = "expired"
	MaxPendingPerWindow = 5

	defaultExpiry     = 24 * time.Hour
	terminalRetention = 14 * 24 * time.Hour
	lockWait          = 5 * time.Second
	lockStale         = 30 * time.Second
)

type Entry struct {
	ID             string        `json:"id"`
	Article        model.Article `json:"article"`
	TargetAt       time.Time     `json:"target_at"`
	CreatedAt      time.Time     `json:"created_at"`
	ExpiresAt      time.Time     `json:"expires_at"`
	Status         string        `json:"status"`
	ConsumedAt     time.Time     `json:"consumed_at,omitzero"`
	ConsumedOutput string        `json:"consumed_output,omitempty"`
}

type State struct {
	SchemaVersion int       `json:"schema_version"`
	UpdatedAt     time.Time `json:"updated_at"`
	Entries       []Entry   `json:"entries"`
}

type Store struct {
	Path string
	Now  func() time.Time
}

func NewStore(path string, now func() time.Time) Store {
	if now == nil {
		now = time.Now
	}
	return Store{Path: path, Now: now}
}

func (s Store) Add(ctx context.Context, targetAt time.Time, article model.Article) (Entry, bool, error) {
	if targetAt.IsZero() {
		return Entry{}, false, fmt.Errorf("carryover target must not be zero")
	}
	article.Link = strings.TrimSpace(article.Link)
	article.Title = strings.TrimSpace(article.Title)
	article.Source = strings.TrimSpace(article.Source)
	article.Category = strings.TrimSpace(article.Category)
	if article.Link == "" || article.Title == "" || article.Source == "" || article.Category == "" || article.Published.IsZero() {
		return Entry{}, false, fmt.Errorf("carryover article requires link, title, source, category, and published time")
	}
	entry := Entry{
		ID:        entryID(article.Link, targetAt),
		Article:   article,
		TargetAt:  targetAt,
		CreatedAt: s.Now(),
		ExpiresAt: targetAt.Add(defaultExpiry),
		Status:    StatusPending,
	}
	created := false
	err := s.mutate(ctx, func(state *State, now time.Time) (bool, error) {
		for _, existing := range state.Entries {
			if existing.ID == entry.ID {
				entry = existing
				return false, nil
			}
		}
		pending := 0
		for _, existing := range state.Entries {
			if existing.Status == StatusPending && existing.TargetAt.Equal(targetAt) && now.Before(existing.ExpiresAt) {
				pending++
			}
		}
		if pending >= MaxPendingPerWindow {
			return false, fmt.Errorf("carryover target already has %d pending entries", MaxPendingPerWindow)
		}
		state.Entries = append(state.Entries, entry)
		created = true
		return true, nil
	})
	return entry, created, err
}

func (s Store) List() ([]Entry, error) {
	state, err := s.read()
	if err != nil {
		return nil, err
	}
	entries := append([]Entry(nil), state.Entries...)
	now := s.Now()
	for index := range entries {
		if entries[index].Status == StatusPending && !now.Before(entries[index].ExpiresAt) {
			entries[index].Status = StatusExpired
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if !entries[i].TargetAt.Equal(entries[j].TargetAt) {
			return entries[i].TargetAt.Before(entries[j].TargetAt)
		}
		return entries[i].CreatedAt.Before(entries[j].CreatedAt)
	})
	return entries, nil
}

func (s Store) PendingFor(targetAt time.Time) ([]Entry, error) {
	entries, err := s.List()
	if err != nil {
		return nil, err
	}
	now := s.Now()
	pending := make([]Entry, 0)
	for _, entry := range entries {
		if entry.Status != StatusPending || !entry.TargetAt.Equal(targetAt) || !now.Before(entry.ExpiresAt) {
			continue
		}
		pending = append(pending, entry)
	}
	return pending, nil
}

func (s Store) Consume(ctx context.Context, ids []string, outputPath string) error {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			wanted[id] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	return s.mutate(ctx, func(state *State, now time.Time) (bool, error) {
		changed := false
		for index := range state.Entries {
			entry := &state.Entries[index]
			if _, ok := wanted[entry.ID]; !ok {
				continue
			}
			if entry.Status == StatusPending {
				entry.Status = StatusConsumed
				entry.ConsumedAt = now
				entry.ConsumedOutput = outputPath
				changed = true
			}
			delete(wanted, entry.ID)
		}
		return changed, nil
	})
}

func (s Store) Remove(ctx context.Context, id string) (bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return false, fmt.Errorf("carryover id must not be empty")
	}
	removed := false
	err := s.mutate(ctx, func(state *State, _ time.Time) (bool, error) {
		entries := state.Entries[:0]
		for _, entry := range state.Entries {
			if entry.ID == id {
				removed = true
				continue
			}
			entries = append(entries, entry)
		}
		state.Entries = entries
		return removed, nil
	})
	return removed, err
}

func (s Store) read() (State, error) {
	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return State{SchemaVersion: SchemaVersion, Entries: []Entry{}}, nil
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("decode carryover state: %w", err)
	}
	if state.SchemaVersion != SchemaVersion {
		return State{}, fmt.Errorf("unsupported carryover schema_version %d", state.SchemaVersion)
	}
	return state, nil
}

func (s Store) mutate(ctx context.Context, mutate func(*State, time.Time) (bool, error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lockPath := s.Path + ".lock"
	if err := acquireLock(ctx, lockPath); err != nil {
		return err
	}
	defer func() { _ = os.Remove(lockPath) }()

	state, err := s.read()
	if err != nil {
		return err
	}
	now := s.Now()
	prune(&state, now)
	changed, err := mutate(&state, now)
	if err != nil || !changed {
		return err
	}
	state.SchemaVersion = SchemaVersion
	state.UpdatedAt = now
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return statefile.WriteAtomic(s.Path, append(data, '\n'), 0o644)
}

func prune(state *State, now time.Time) {
	entries := state.Entries[:0]
	for _, entry := range state.Entries {
		if entry.Status == StatusPending && !now.Before(entry.ExpiresAt) {
			entry.Status = StatusExpired
		}
		terminalAt := entry.ConsumedAt
		if entry.Status == StatusExpired {
			terminalAt = entry.ExpiresAt
		}
		if entry.Status != StatusPending && !terminalAt.IsZero() && now.Sub(terminalAt) > terminalRetention {
			continue
		}
		entries = append(entries, entry)
	}
	state.Entries = entries
}

func acquireLock(ctx context.Context, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	deadline := time.Now().Add(lockWait)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(path)
				return closeErr
			}
			return nil
		}
		if !os.IsExist(err) {
			return err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > lockStale {
			if removeErr := os.Remove(path); removeErr == nil || os.IsNotExist(removeErr) {
				continue
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for carryover state lock")
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func entryID(rawURL string, targetAt time.Time) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawURL) + "\n" + targetAt.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(sum[:8])
}
