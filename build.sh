#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

echo "Building..."
go build -o news-briefing ./cmds/news-briefing/

if [[ "$(uname -s)" == "Darwin" ]]; then
	identity="${NEWS_BRIEFING_CODESIGN_IDENTITY:-}"
	required="${NEWS_BRIEFING_CODESIGN_REQUIRED:-}"
	identifier="${NEWS_BRIEFING_CODESIGN_IDENTIFIER:-com.news-briefing.briefing}"

	if [[ -n "$identity" ]]; then
		echo "Signing ./news-briefing with identity: $identity"
		codesign --force --sign "$identity" --identifier "$identifier" --timestamp=none ./news-briefing
		codesign --verify --verbose=2 ./news-briefing
	elif [[ "$required" == "1" ]]; then
		echo "Error: NEWS_BRIEFING_CODESIGN_REQUIRED=1 but NEWS_BRIEFING_CODESIGN_IDENTITY is not set." >&2
		exit 1
	else
		echo "Skipping local macOS signing. Set NEWS_BRIEFING_CODESIGN_IDENTITY to sign ./news-briefing."
	fi
elif [[ "${NEWS_BRIEFING_CODESIGN_REQUIRED:-}" == "1" ]]; then
	echo "Error: NEWS_BRIEFING_CODESIGN_REQUIRED=1 but local macOS signing is only available on Darwin." >&2
	exit 1
fi

echo "Done. Binary: ./news-briefing"
