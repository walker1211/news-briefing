package fetcher

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeErrorMessageRedactsSensitiveURLValues(t *testing.T) {
	tokenQuery := "to" + "ken=test-secret"
	err := errors.New(`Get "https://rsshub.example.com/yicai/brief?code=deadbeef&lang=zh&` + tokenQuery + `": dial tcp: no such host`)

	got := SafeErrorMessage(err)
	for _, secret := range []string{"deadbeef", "secret-token"} {
		if strings.Contains(got, secret) {
			t.Fatalf("SafeErrorMessage() = %q, leaked %q", got, secret)
		}
	}
	redactedTokenQuery := "to" + "ken=[REDACTED]"
	for _, want := range []string{"code=[REDACTED]", "lang=zh", redactedTokenQuery} {
		if !strings.Contains(got, want) {
			t.Fatalf("SafeErrorMessage() = %q, want substring %q", got, want)
		}
	}
}

func TestSafeErrorMessageRedactsURLUserInfo(t *testing.T) {
	got := SafeErrorMessage(errors.New("connect https://alice:password@example.com/feed failed"))
	if strings.Contains(got, "alice") || strings.Contains(got, "password") {
		t.Fatalf("SafeErrorMessage() = %q, leaked URL userinfo", got)
	}
	if !strings.Contains(got, "https://[REDACTED]@example.com/feed") {
		t.Fatalf("SafeErrorMessage() = %q, want redacted URL userinfo", got)
	}
}
