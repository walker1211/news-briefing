# International News Briefing Aggregator

A configurable news briefing tool that fetches news from multiple sources, filters by keywords and time windows, and generates Chinese briefings through your local AI CLI.

[中文文档](./README.zh-CN.md) | [English Documentation](./README.en.md)

## Features

- Fetch from RSS, Hacker News, Reddit, and page sources
- Filter by configured keywords and time windows
- Generate Chinese briefings through your local AI CLI
- Output to terminal, Markdown, and optional email
- Support scheduled runs and manual regeneration
- Keep seen-state for deduplication

## Quick Start

```bash
cp configs/config.example.yaml configs/config.yaml
./build.sh
./news-briefing help
```

Fill in `configs/config.yaml` before running non-help commands. Add `.env` only when email delivery is enabled.

## Common Commands

```bash
./news-briefing run
./news-briefing fetch --zh
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00"
./news-briefing deep "OpenAI"
./news-briefing resend-md --file output/26.04.13-晚间-1800.md
./news-briefing serve
```

## License

Licensed under the MIT License. See [LICENSE](./LICENSE).
