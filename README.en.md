# International News Briefing Aggregator

[Landing Page](./README.md) | [中文](./README.zh-CN.md)

## Installation

#### Option 1: Download from GitHub Releases

GitHub Releases currently provide macOS and Linux archives only.

1. Download the archive for your platform from GitHub Releases.
2. Extract it into a working directory.
3. Copy `configs/config.example.yaml` to `configs/config.yaml`.
4. Fill in `configs/config.yaml` for your sources and AI CLI.
5. Run `./news-briefing help` from that working directory.
6. Add `.env` only if you plan to use email sending.

Note: the binary currently reads `configs/config.yaml` and `.env` from the current working directory. Adding the binary to `PATH` does not remove that requirement.

#### Option 2: Build from source

Requires Go 1.25 or newer.

```bash
cp configs/config.example.yaml configs/config.yaml
./build.sh
./news-briefing help
```

Before running non-help commands such as `run`, fill in `configs/config.yaml` for your sources and AI CLI. Add `.env` when email sending is enabled.

## Configuration

### 1. Put secrets in `.env`

At the moment, you only need to put the email auth code there:

```dotenv
EMAIL_SMTP_AUTH_CODE=your-email-auth-code
```

Rules:

- keep only sensitive values in `.env`
- keep structured configuration in YAML

### 2. Template config and local config

Example email delivery config:

```yaml
email:
  smtp_host: smtp.example.com
  smtp_port: 465
  from: from@example.com
  to: to@example.com
  timeout: 3s
  retry_times: 3
  retry_wait_time: 500ms
  use_proxy: false
```

Notes:

- `timeout`: SMTP connect/send timeout
- `retry_times`: total send attempts
- `retry_wait_time`: wait duration between retries
- `use_proxy`: whether email sending should use the configured SOCKS5 proxy

Repository config layout:

- `configs/config.example.yaml`: template config committed to git
- `configs/config.yaml`: real local config, not committed to git

Initialization:

```bash
cp configs/config.example.yaml configs/config.yaml
```

Then edit `configs/config.yaml` and fill in:

- email addresses
- sources and keywords
- scheduled trigger times in `schedule`
- output directory in `output.dir`
- proxy settings
- AI CLI command and batch flags (default `ccs codex` with non-interactive flags)

Example source categories:

```yaml
sources:
  - name: Example AI Feed
    url: https://example.com/ai.rss
    type: rss
    category: AI/科技
  - name: Example Open Source Feed
    url: https://example.com/open-source.rss
    type: rss
    category: 开源工具
  - name: Example Startup Feed
    url: https://example.com/startups.rss
    type: rss
    category: 商业/公司
  - name: Example World Feed
    url: https://example.com/world.rss
    type: rss
    category: 国际政治
```

Notes:

- `category` can be any string
- grouped output follows the first-appearance order of `sources`
- if a runtime category is not present in config, it is appended after configured categories

The program reads only `configs/config.yaml` by default.

### 3. AI CLI

Make sure the AI CLI configured in `configs/config.yaml` is available and already logged in. This project runs large models through a locally logged-in AI CLI and does not require storing API keys inside this repository. Default template example:

```yaml
ai:
  command: ccs
  args:
    - codex
  extra_flags:
    - --bare
    - --disable-slash-commands
  append_system_prompt: true
```

If you only have `claude` installed, you can usually switch to:

```yaml
ai:
  command: claude
  args: []
  extra_flags:
    - --bare
    - --disable-slash-commands
  append_system_prompt: true
```

Do not put `-p` into `args`; the program appends the prompt argument automatically.

### 4. output configuration

```yaml
output:
  dir: output
  mode: translated_only
  include_filtered_articles: false
```

Allowed `mode` values:

- `original_only` — show only the original/raw article block
- `translated_only` — show only the Chinese AI-generated block
- `bilingual_translated_first` — show Chinese first, then the original block
- `bilingual_original_first` — show the original block first, then the Chinese block

`include_filtered_articles` defaults to `false`. When set to `true`, `run`, `regen`, and `serve` append in-window candidates that did not match any keyword to the end of the briefing. These candidates are not included in the AI summary and are not written to the seen state.

This setting affects:

- `run`
- `regen`
- `serve` (it only reuses the briefing output pipeline; it does not add a server endpoint)
- `fetch --zh`
- `deep`

Exception:

- plain `./news-briefing fetch` still prints the raw article list directly and does not use `output.mode`

## Commands

Build first:

```bash
./build.sh
```

Common commands:

```bash
./news-briefing help
./news-briefing run
./news-briefing run --raw
./news-briefing run --no-email
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00"
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00" --period 1400 --ignore-seen --send-email
./news-briefing fetch
./news-briefing fetch --zh
./news-briefing deep "OpenAI"
./news-briefing deep "Claude" --ignore-seen
./news-briefing resend-md --file output/26.04.13-晚间-1800.md
./news-briefing serve
```

## Command reference

### `run`

Generate a regular briefing:

- fetch news
- generate a Chinese summary and 3 follow-up directions worth tracking
- print to terminal
- write Markdown
- send email by default

Optional flags:

- `--raw`: also print the raw article list
- `--no-email`: skip email delivery

### `regen`

Regenerate a briefing for an explicit time window. Useful for backfilling, reruns, and resending:

```bash
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00"
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00" --ignore-seen
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00" --period 1400 --ignore-seen --send-email
```

Rules:

- `--from` / `--to` are required and parsed in `schedule_timezone`; if `schedule_timezone` is not configured, the system local timezone is used. Format: `YYYY-MM-DD HH:MM`
- `--to` must be later than or equal to `--from`
- `--period` is optional; when omitted, it defaults to the `HHMM` of `--to`
- `--ignore-seen` skips the persisted seen-state filter and only keeps in-batch deduplication
- `--send-email` is off by default and only sends mail when explicitly passed
- `regen` still writes a Markdown file by default

### `fetch`

Fetch news without generating a summary:

- `./news-briefing fetch`: print the raw article list
- `./news-briefing fetch --zh`: additionally call the configured AI CLI to output Chinese translation

### `deep`

Generate a topic deep-dive pack around a keyword or company. The `deep` commands emitted by the daily briefing also include `--ignore-seen` by default so historical seen-state does not hide useful follow-up items too early:

```bash
./news-briefing deep "OpenAI"
./news-briefing deep "Claude" --ignore-seen
./news-briefing deep "Claude" --from "2026-03-28 00:00" --to "2026-03-29 23:59"
./news-briefing deep "Claude" --from "2026-03-28 00:00" --to "2026-03-29 23:59" --ignore-seen
```

Rules:

- `--from` / `--to` are optional and parsed in `schedule_timezone`; when omitted, the system local timezone is used. Format: `YYYY-MM-DD HH:MM`
- `--from` / `--to` must be omitted together or provided together
- `--to` must be later than or equal to `--from`
- when `--from` / `--to` are omitted, `deep` reads from the unread pool; if only `--ignore-seen` is passed, it uses the most recent 12-hour window
- `--ignore-seen` skips the persisted seen-state filter and only keeps in-batch deduplication

### `resend-md`

Resend email from an existing Markdown file without fetching news again and without regenerating AI output:

```bash
./news-briefing resend-md --file output/26.04.13-晚间-1800.md
```

Rules:

- `--file` is required
- the file must be a `.md` file
- the file path must stay under `output.dir`
- the recipient uses the current `email.to`
- the email subject is derived from the Markdown filename; for example, `26.04.13-晚间-1800.md` becomes `国际资讯简报 26.04.13 晚间 18:00`

### `serve`

Daemon mode. Runs automatically based on `schedule` in `configs/config.yaml` and uses the same output pipeline as `run`.

Current example template:

```yaml
schedule:
  - "0 8 * * *"
  - "0 14 * * *"
```

Note: the scheduled fetch window is derived by taking the current trigger time and walking back to the previous planned time point in the current `schedule`. Missed historical trigger points are not automatically backfilled after a restart. Because of that, changing `schedule` can leave a gap if you introduce a new time point that has already passed today but was never actually executed by the service.

Recommendation: after changing cron / `schedule`, if you suspect a gap, use the built-in `regen --from --to` command to backfill that window manually, for example:

```bash
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00"
```

If you only move a future time point later or otherwise adjust a not-yet-triggered slot, you usually will not create a gap, but it is still worth checking the generated output on the day of the change.

## Output and state files

With the default `output.dir=output`:

- briefing Markdown: `output/26.03.18-午间-1400.md`
- deep-dive material: `output/deep/26.03.18-topic.md`
- seen state: `output/state/seen.json`

Example email subjects:

- regular briefing email: `[资讯简报] 26.03.18 午间 14:00`
- `resend-md` email: `国际资讯简报 26.03.18 午间 14:00`

## Scheduled running

### Option 1: helper scripts

```bash
./start.sh
./stop.sh
./restart.sh
```

When using macOS `launchd` to supervise the service, use these helpers instead. They assume `~/Library/LaunchAgents/com.news-briefing.briefing.plist` and the Label `com.news-briefing.briefing`:

```bash
./launch-start.sh
./launch-stop.sh
./launch-restart.sh
./launch-status.sh
```

### Option 2: macOS launchd

`launchd` can supervise a long-running `./news-briefing serve` process. The actual trigger times still come from `schedule` in `configs/config.yaml`.

Create `~/Library/LaunchAgents/com.news-briefing.briefing.plist` and have it execute `./news-briefing serve` from this repository. Because the program reads `configs/config.yaml` and `.env` from the current working directory, set `WorkingDirectory` explicitly in the plist.

Minimal example:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.news-briefing.briefing</string>

  <key>WorkingDirectory</key>
  <string>/absolute/path/to/news-briefing</string>

  <key>ProgramArguments</key>
  <array>
    <string>/absolute/path/to/news-briefing/news-briefing</string>
    <string>serve</string>
  </array>

  <key>RunAtLoad</key>
  <true/>

  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>/absolute/path/to/news-briefing/logs/out.log</string>

  <key>StandardErrorPath</key>
  <string>/absolute/path/to/news-briefing/logs/err.log</string>
</dict>
</plist>
```

Common commands:

```bash
mkdir -p logs
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.news-briefing.briefing.plist 2>/dev/null || true
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.news-briefing.briefing.plist
launchctl kickstart -k gui/$(id -u)/com.news-briefing.briefing
launchctl print gui/$(id -u)/com.news-briefing.briefing
```

Stop and unload:

```bash
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.news-briefing.briefing.plist
```

## Development / testing

```bash
go test ./...
./build.sh
```

## License

Licensed under the MIT License. See [LICENSE](./LICENSE).
