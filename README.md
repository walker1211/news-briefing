# 国际资讯聚合器 / International News Briefing Aggregator

[中文](#中文) | [English](#english)

自动抓取 AI/科技 与 国际政治新闻，通过已配置的 AI CLI 生成中文简报、今日最值得追的 3 个方向与话题深挖包，支持终端输出、Markdown
落盘和邮件发送。

Fetch AI/tech and world-politics news, then use a configurable AI CLI (such as `ccs` or `claude`) to generate Chinese
briefings, follow-up directions, and topic deep-dive packs.

## English

A configurable news briefing tool for personal or self-hosted workflows.
It runs LLMs through installed AI CLI tools, so you can use logged-in CLI sessions instead of managing API keys inside this project.

### What it does

- Fetches news from RSS, Hacker News, and Reddit sources
- Filters by your configured keywords and time windows
- Generates:
    - Chinese briefings
    - 3 follow-up directions worth tracking
    - topic deep-dive packs
- Outputs to terminal, Markdown, and optional email

### Requirements

- Go 1.25 or newer

### Quick start

1. Copy the example config:

```bash
cp configs/config.example.yaml configs/config.yaml
```

2. Put your email smtp auth code in `.env`:

```dotenv
EMAIL_SMTP_AUTH_CODE=mail_smtp_password
```

3. Configure your AI CLI in `configs/config.yaml`.

Default `ccs` example:

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

If you only have `claude` installed:

```yaml
ai:
  command: claude
  args: [ ]
  extra_flags:
    - --bare
    - --disable-slash-commands
  append_system_prompt: true
```

Do not put `-p` into `args`; the program appends the prompt argument automatically.

### Output modes

```yaml
output:
  dir: output
  mode: translated_only
```

Allowed values:

- `original_only` — show only the original/raw article block
- `translated_only` — show only the Chinese AI-generated block
- `bilingual_translated_first` — show Chinese first, then the original block
- `bilingual_original_first` — show the original block first, then the Chinese block

This setting affects `run`, `regen`, scheduled `serve`, `fetch --zh`, and `deep`.
Plain `fetch` keeps printing the original article list and does not use the output-mode formatter.

4. Build and run:

```bash
./build.sh
./news-briefing help
./news-briefing run --no-email
```

### Common commands

```bash
./news-briefing run
./news-briefing run --raw
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00" --ignore-seen
./news-briefing fetch --zh
./news-briefing deep "OpenAI"
./news-briefing serve
```

### Development / testing

```bash
go test ./...
./build.sh
```

### License

Licensed under Apache License 2.0. See [LICENSE](./LICENSE).

## 中文

### 环境要求

- Go 1.25 或更高版本

### 配置

### 1. 敏感信息放在 `.env`

目前只需要放邮箱授权码：

```dotenv
EMAIL_SMTP_AUTH_CODE=邮箱授权码
```

原则：

- `.env` 只放敏感值
- 结构化配置放 YAML

### 2. 模板配置与真实配置

仓库内配置布局：

- `configs/config.example.yaml`：模板配置，提交到版本库
- `configs/config.yaml`：本地真实配置，不提交到版本库

初始化方式：

```bash
cp configs/config.example.yaml configs/config.yaml
```

然后编辑 `configs/config.yaml`，填写：

- 邮箱地址
- 新闻源与关键词
- 调度时间 `schedule`
- 输出目录 `output.dir`
- 代理配置
- AI CLI 命令与批处理 flags（默认 `ccs codex` + 非交互 flags）

程序默认只读取 `configs/config.yaml`。

### 3. AI CLI

确保 `configs/config.yaml` 中配置的 AI CLI 可用并已登录。项目通过本机已登录的 AI CLI 调用大模型，不依赖在本项目里额外配置 API Key。默认模板使用：

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

如果你本机只有 `claude`，通常只需要改成：

```yaml
ai:
  command: claude
  args: []
  extra_flags:
    - --bare
    - --disable-slash-commands
  append_system_prompt: true
```

注意：这里不要把 `-p` 写进 `args`，程序会在运行时自动追加 prompt 参数。

### 4. output.mode 输出模式

示例：

```yaml
output:
  dir: output
  mode: translated_only
```

允许值：

- `original_only`：只显示原文/原始文章整理块
- `translated_only`：只显示 AI 生成的中文内容块
- `bilingual_translated_first`：先显示中文，再显示原文
- `bilingual_original_first`：先显示原文，再显示中文

作用范围：

- `run`
- `regen`
- `serve`（只是复用 briefing 输出链路，不是新增服务端 endpoint）
- `fetch --zh`
- `deep`

例外：

- plain `./news-briefing fetch` 仍然直接输出原始文章列表，不受 `output.mode` 影响

## 命令

先构建：

```bash
./build.sh
```

常用命令：

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
./news-briefing serve
```

## 命令说明

### `run`

生成简报：

- 抓取新闻
- 生成中文摘要与选题建议
- 输出到终端
- 写入 Markdown
- 默认发送邮件

可选参数：

- `--raw`：同时显示原始文章列表
- `--no-email`：跳过邮件发送

### `regen`

按指定时间窗重生成简报，适合补档、重跑和补发：

```bash
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00"
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00" --ignore-seen
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00" --period 1400 --ignore-seen --send-email
```

规则：

- `--from` / `--to` 必填，按 `schedule_timezone` 解析；未配置时使用系统本地时区，格式 `YYYY-MM-DD HH:MM`
- `--to` 必须晚于或等于 `--from`
- `--period` 可选；不传时默认取 `--to` 的 `HHMM`
- `--ignore-seen` 会跳过持久化已读状态，仅做当前批次内去重
- `--send-email` 默认关闭，显式传入时才发邮件
- `regen` 默认仍会写出 Markdown 文件

### `fetch`

仅抓取新闻，不生成摘要：

- `./news-briefing fetch`：显示原始文章列表
- `./news-briefing fetch --zh`：额外调用已配置 AI CLI 输出中文翻译

### `deep`

围绕指定话题生成话题深挖包：

```bash
./news-briefing deep "OpenAI"
```

### `serve`

守护模式，按 `configs/config.yaml` 中的 `schedule` 自动执行，与 `run` 使用同一输出链路。

模板配置当前示例：

```yaml
schedule:
  - "0 8 * * *"
  - "0 14 * * *"
```

## 输出与状态文件

默认 `output.dir=output` 时：

- 简报 Markdown：`output/26.03.18-午间-1400.md`
- 深挖素材：`output/deep/26.03.18-topic.md`
- 已读状态：`output/state/seen.json`

邮件主题示例：

- `[资讯简报] 26.03.18 午间 14:00`

## 定时运行

### 方式一：脚本启停

```bash
./start.sh
./stop.sh
./restart.sh
```

### 方式二：macOS launchd

launchd 可以托管常驻的 `./news-briefing serve` 进程，实际触发时间仍以 `configs/config.yaml` 中的 `schedule` 为准。

版本库不再跟踪 `com.news-briefing.briefing.plist` 模板；如果你本地已有历史 plist 文件，可以继续按本机路径调整后使用。否则请自行创建
`~/Library/LaunchAgents/com.news-briefing.briefing.plist`，让它执行当前仓库里的 `./news-briefing serve`。

常用命令：

```bash
mkdir -p logs
launchctl load ~/Library/LaunchAgents/com.news-briefing.briefing.plist
launchctl list | grep news-briefing
launchctl unload ~/Library/LaunchAgents/com.news-briefing.briefing.plist
```

## 开发 / 测试

```bash
go test ./...
./build.sh
```

## License

本项目使用 Apache License 2.0，详见 [LICENSE](./LICENSE)。
