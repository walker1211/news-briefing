package carryover

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/walker1211/news-briefing/internal/model"
)

func testArticle(link string) model.Article {
	return model.Article{Title: "Important update", Link: link, Summary: "Details", Source: "X/@team", Category: "AI/科技", Published: time.Date(2026, 8, 17, 4, 12, 0, 0, time.Local)}
}

func TestStoreAddIsIdempotentAndLimitsPendingPerWindow(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	target := now.Add(6 * time.Hour)
	store := NewStore(filepath.Join(t.TempDir(), "carryover.json"), func() time.Time { return now })

	first, created, err := store.Add(context.Background(), target, testArticle("https://example.com/1"))
	if err != nil || !created {
		t.Fatalf("first Add() = %#v, %v, %v", first, created, err)
	}
	duplicate, created, err := store.Add(context.Background(), target, testArticle("https://example.com/1"))
	if err != nil || created || duplicate.ID != first.ID {
		t.Fatalf("duplicate Add() = %#v, %v, %v", duplicate, created, err)
	}
	for index := 2; index <= MaxPendingPerWindow; index++ {
		if _, _, err := store.Add(context.Background(), target, testArticle(fmt.Sprintf("https://example.com/%d", index))); err != nil {
			t.Fatalf("Add(%d) error = %v", index, err)
		}
	}
	if _, _, err := store.Add(context.Background(), target, testArticle("https://example.com/overflow")); err == nil {
		t.Fatal("overflow Add() error = nil")
	}
}

func TestStoreConsumesOnlyPendingTargetEntries(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	target := now.Add(6 * time.Hour)
	store := NewStore(filepath.Join(t.TempDir(), "carryover.json"), func() time.Time { return now })
	entry, _, err := store.Add(context.Background(), target, testArticle("https://example.com/1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Consume(context.Background(), []string{entry.ID}, "briefing.md"); err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if err := store.Consume(context.Background(), []string{entry.ID, "already-removed"}, "briefing.md"); err != nil {
		t.Fatalf("idempotent Consume() error = %v", err)
	}
	pending, err := store.PendingFor(target)
	if err != nil || len(pending) != 0 {
		t.Fatalf("PendingFor() = %#v, %v", pending, err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != 1 || entries[0].Status != StatusConsumed || entries[0].ConsumedOutput != "briefing.md" {
		t.Fatalf("List() = %#v, %v", entries, err)
	}
}

func TestStoreExpiresPendingEntries(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	target := now.Add(time.Hour)
	store := NewStore(filepath.Join(t.TempDir(), "carryover.json"), func() time.Time { return now })
	if _, _, err := store.Add(context.Background(), target, testArticle("https://example.com/1")); err != nil {
		t.Fatal(err)
	}
	now = target.Add(defaultExpiry + time.Second)
	if _, _, err := store.Add(context.Background(), now.Add(time.Hour), testArticle("https://example.com/2")); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List()
	if err != nil || len(entries) != 2 || entries[0].Status != StatusExpired {
		t.Fatalf("List() = %#v, %v", entries, err)
	}
}

func TestStoreSerializesConcurrentAdds(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	target := now.Add(6 * time.Hour)
	store := NewStore(filepath.Join(t.TempDir(), "carryover.json"), func() time.Time { return now })
	var wg sync.WaitGroup
	errs := make(chan error, MaxPendingPerWindow)
	for index := 0; index < MaxPendingPerWindow; index++ {
		index := index
		wg.Go(func() {
			_, _, err := store.Add(context.Background(), target, testArticle(fmt.Sprintf("https://example.com/%d", index)))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Add() error = %v", err)
		}
	}
	entries, err := store.PendingFor(target)
	if err != nil || len(entries) != MaxPendingPerWindow {
		t.Fatalf("PendingFor() = %d, %v", len(entries), err)
	}
}
