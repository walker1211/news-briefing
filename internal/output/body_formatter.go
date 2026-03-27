package output

import (
	"fmt"
	"strings"

	"github.com/walker1211/news-briefing/internal/model"
)

const bilingualBodySeparator = "\n\n---\n\n"

func FormatBody(commandPath string, mode model.OutputMode, content model.OutputContent) (string, error) {
	switch mode {
	case model.OutputModeOriginalOnly:
		original, err := requireBody(commandPath, mode, "original", content.Original)
		if err != nil {
			return "", err
		}
		return original, nil
	case model.OutputModeTranslatedOnly:
		translated, err := requireBody(commandPath, mode, "translated", content.Translated)
		if err != nil {
			return "", err
		}
		return translated, nil
	case model.OutputModeBilingualTranslatedFirst:
		translated, err := requireBody(commandPath, mode, "translated", content.Translated)
		if err != nil {
			return "", err
		}
		original, err := requireBody(commandPath, mode, "original", content.Original)
		if err != nil {
			return "", err
		}
		return translated + bilingualBodySeparator + original, nil
	case model.OutputModeBilingualOriginalFirst:
		original, err := requireBody(commandPath, mode, "original", content.Original)
		if err != nil {
			return "", err
		}
		translated, err := requireBody(commandPath, mode, "translated", content.Translated)
		if err != nil {
			return "", err
		}
		return original + bilingualBodySeparator + translated, nil
	default:
		return "", fmt.Errorf("%s: output.mode=%q invalid mode", commandPath, mode)
	}
}

func requireBody(commandPath string, mode model.OutputMode, side, body string) (string, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed != "" {
		return trimmed, nil
	}
	return "", fmt.Errorf("%s: output.mode=%q requires %s content, but %s is missing", commandPath, mode, side, side)
}
