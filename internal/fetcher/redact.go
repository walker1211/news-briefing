package fetcher

import "regexp"

var (
	secretQueryValuePattern = regexp.MustCompile(`(?i)([?&](?:access[_-]?key|api[_-]?key|apikey|code|key|password|secret|token)=)[^&\s"']+`)
	urlUserInfoPattern      = regexp.MustCompile(`(?i)(https?://)[^/@\s:]+:[^/@\s]+@`)
)

// SafeErrorMessage returns an error message that is safe to print, persist, or
// include in email. Upstream HTTP libraries often include the complete request
// URL in an error, including RSSHub access codes.
func SafeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	message := urlUserInfoPattern.ReplaceAllString(err.Error(), `${1}[REDACTED]@`)
	return secretQueryValuePattern.ReplaceAllString(message, `${1}[REDACTED]`)
}

func (f FailedSource) SafeErrorMessage() string {
	return SafeErrorMessage(f.Err)
}
