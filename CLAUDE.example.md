# CLAUDE.example.md

Claude Code 在本仓库中工作时可参考以下公开说明。

## 常用命令

以下命令均默认在仓库根目录执行。

```bash
go build -o ./news-briefing ./cmds/news-briefing
./news-briefing help
go test ./...
go test ./internal/config -run TestLoadAppliesDefaultAIConfig -v
./news-briefing run
./news-briefing run --no-email
./news-briefing fetch
./news-briefing fetch --zh
./news-briefing deep "OpenAI"
./news-briefing serve
./start.sh
./stop.sh
./restart.sh
```

## 关键路径

- `cmds/news-briefing/main.go`：CLI 启动层，仅负责配置加载、依赖装配、参数错误输出与统一退出处理。
- `cmds/news-briefing/cli.go`：命令解析层，负责 `run`、`regen`、`fetch`、`serve`、`deep` 参数解析。
- `cmds/news-briefing/execute.go`：执行层，负责命令分发、scheduler 注入、runner 注入以及 run/regen 共用渲染链路。
- `internal/config/config.go`：加载 `.env` 与 `configs/config.yaml`；默认值包括 `output.dir=output`、`ai.command=ccs`、
  `ai.args=codex`。
- `internal/fetcher/`：负责各来源抓取、重试策略、排序、去重与 seen-state 存储边界。
- `internal/summarizer/claude.go`：负责实例化 AI runner、设置代理环境变量、清洗 CLI 输出。
- `internal/output/`：负责终端、Markdown 和邮件输出。
- `internal/scheduler/cron.go`：负责 `Asia/Shanghai` 时区下的 cron 调度与 period 推导。

## 关键提醒

- AI 命令通过 `configs/config.yaml` 配置，不要硬编码 `claude`。
- `.env` 只放敏感值；真实配置放 `configs/config.yaml`；模板配置放 `configs/config.example.yaml`。
- seen 状态默认写入 `<output.dir>/state/seen.json`；默认路径是 `output/state/seen.json`。
- 如果修改了 CLI 流程，也要验证 `serve` 模式和输出格式。
- Reddit 抓取故意采用串行模式，并在 subreddit 之间保留 2 秒间隔；不要随意提高重试频率或并发度。
- `sanitizeCLIOutput(...)` 仅适用于 `ccs codex` 路径。
- `periodPrefix()` 是时间段标签的唯一事实来源。
- Briefing 文件名格式为 `YY.MM.DD-<凌晨|早间|午间|晚间>-HHMM.md`。
- 如果发现二进制行为与代码不一致，调试前先重新构建 `./news-briefing`。
- `.claude/memory/` 是项目共享记忆的主源，稳定的项目约定应维护在这里。
