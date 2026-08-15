package sourcehealth

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestUpdateAlertsAfterTwoDistinctWindowsAndReportsRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source-health.json")
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	issues := []Issue{{Key: "fetch:The Diplomat", Name: "The Diplomat"}}

	first, err := Update(path, "0800-1", now, 2, issues)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.VisibleKeys) != 0 || len(first.Recoveries) != 0 {
		t.Fatalf("first = %#v, want silent first failure", first)
	}

	repeated, err := Update(path, "0800-1", now.Add(time.Minute), 2, issues)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.VisibleKeys) != 0 {
		t.Fatalf("same-window retry must not increment: %#v", repeated)
	}

	second, err := Update(path, "1800-1", now.Add(10*time.Hour), 2, issues)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.VisibleKeys, []string{"fetch:The Diplomat"}) {
		t.Fatalf("second.VisibleKeys = %#v", second.VisibleKeys)
	}

	recovered, err := Update(path, "0800-2", now.Add(24*time.Hour), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered.Recoveries, []string{"The Diplomat"}) {
		t.Fatalf("recovered.Recoveries = %#v", recovered.Recoveries)
	}
}

func TestUpdateDoesNotReportRecoveryForUnalertedSingleFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source-health.json")
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	if _, err := Update(path, "one", now, 2, []Issue{{Key: "watch:Support", Name: "Support"}}); err != nil {
		t.Fatal(err)
	}
	result, err := Update(path, "two", now.Add(time.Hour), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recoveries) != 0 {
		t.Fatalf("Recoveries = %#v, want none", result.Recoveries)
	}
}
