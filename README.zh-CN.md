# 国际资讯聚合器

[入口页](./README.md) | [English](./README.en.md)

## 安装

#### 方式一：从 GitHub Releases 下载

GitHub Releases 当前只提供 macOS 和 Linux 压缩包。

1. 到 GitHub Releases 下载对应平台压缩包。
2. 解压到一个工作目录。
3. 将 `configs/config.example.yaml` 复制为 `configs/config.yaml`。
4. 填写 `configs/config.yaml`，配置新闻源和 AI CLI。
5. 在该工作目录中运行 `./news-briefing help`。
6. 只有在需要发送邮件时才补充 `.env`。

说明：当前程序会从当前工作目录读取 `configs/config.yaml` 和 `.env`。即使把二进制加入 `PATH`，也仍然需要在包含这些文件的工作目录中运行。

#### 方式二：从源码构建

需要 Go 1.25 或更高版本。

```bash
cp configs/config.example.yaml configs/config.yaml
./build.sh
./news-briefing help
```

在运行 `run` 等非 help 命令前，请先填写 `configs/config.yaml`。如需发送邮件，再补充 `.env`。

## 配置

### 1. 敏感信息放在 `.env`

目前只需要放邮箱授权码：

```dotenv
EMAIL_SMTP_AUTH_CODE=邮箱授权码
```

原则：

- `.env` 只放敏感值
- 结构化配置放 YAML

### 2. 模板配置与真实配置

邮件发送相关配置示例：

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

说明：

- `timeout`：SMTP 连接和发送超时
- `retry_times`：总尝试次数
- `retry_wait_time`：重试间隔
- `use_proxy`：是否让邮件发送走已配置的 SOCKS5 代理

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

分类示例：

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

说明：

- `category` 可以自定义，不限于内置枚举
- 分组展示顺序按 `sources` 中首次出现顺序决定
- 如果运行时出现了配置里没有的分类，会追加到已配置分类之后

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
./news-briefing deep "Claude" --ignore-seen
./news-briefing resend-md --file output/26.04.13-晚间-1800.md
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

围绕指定话题生成话题深挖包。每日简报里“今日最值得追的方向”部分给出的 deep 命令，也会默认带上 `--ignore-seen`，避免被历史已读状态过早过滤：

```bash
./news-briefing deep "OpenAI"
./news-briefing deep "Claude" --ignore-seen
./news-briefing deep "Claude" --from "2026-03-28 00:00" --to "2026-03-29 23:59"
./news-briefing deep "Claude" --from "2026-03-28 00:00" --to "2026-03-29 23:59" --ignore-seen
```

规则：

- `--from` / `--to` 可选，按 `schedule_timezone` 解析；未配置时使用系统本地时区，格式 `YYYY-MM-DD HH:MM`
- `--from` / `--to` 要么都不传，要么一起传
- `--to` 必须晚于或等于 `--from`
- 不传 `--from` / `--to` 时，默认读取未读池；若仅传 `--ignore-seen`，则使用最近 12 小时窗口
- `--ignore-seen` 会跳过持久化已读状态，仅做当前批次内去重

### `resend-md`

基于已经生成的 Markdown 文件重发邮件，不重新抓取新闻，也不重新生成摘要：

```bash
./news-briefing resend-md --file output/26.04.13-晚间-1800.md
```

规则：

- `--file` 必填
- 文件必须是 `.md`
- 文件路径必须位于 `output.dir` 下
- 收件人使用当前 `email.to`
- 邮件主题从 Markdown 文件名推导，例如 `26.04.13-晚间-1800.md` 会发成 `国际资讯简报 26.04.13 晚间 18:00`

### `serve`

守护模式，按 `configs/config.yaml` 中的 `schedule` 自动执行，与 `run` 使用同一输出链路。

模板配置当前示例：

```yaml
schedule:
  - "0 8 * * *"
  - "0 14 * * *"
```

注意：`serve` 的抓取窗口是按“当前触发时间”向前回推到“当前 `schedule` 中上一个计划时间点”计算的，不会在重启后自动补跑已经错过的历史时间点。因此如果你修改了 `schedule`，并引入了今天已经过去但服务没有实际执行过的新时间点，下一次定时运行可能出现时间窗断层。

建议：调整 cron / `schedule` 后，如怀疑有断层，优先使用项目自带的 `regen --from --to` 手动补窗，例如：

```bash
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00"
```

如果只是把未来尚未到来的时间点顺延或后移，通常不会产生断层，但仍建议在改动当天关注生成结果是否符合预期。

## 输出与状态文件

默认 `output.dir=output` 时：

- 简报 Markdown：`output/26.03.18-午间-1400.md`
- 深挖素材：`output/deep/26.03.18-topic.md`
- 已读状态：`output/state/seen.json`

邮件主题示例：

- 常规简报邮件：`[资讯简报] 26.03.18 午间 14:00`
- `resend-md` 重发邮件：`国际资讯简报 26.03.18 午间 14:00`

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

本项目使用 MIT License，详见 [LICENSE](./LICENSE)。