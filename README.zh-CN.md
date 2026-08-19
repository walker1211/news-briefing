[English](./README.md)

# 国际资讯聚合器

## 安装

#### 方式一：从 GitHub Releases 下载

GitHub Releases 当前只提供 macOS 和 Linux 压缩包。

1. 到 GitHub Releases 下载对应平台压缩包，例如 `news-briefing_<tag>_<os>_<arch>.tar.gz`。
2. 解压到一个工作目录。
3. 将 `configs/config.example.yaml` 复制为 `configs/config.yaml`。
4. 填写 `configs/config.yaml`，配置新闻源和 AI CLI。
5. 在该工作目录中运行 `./news-briefing --help`。
6. 只有在需要发送邮件时才补充 `.env`。

说明：当前程序会从当前工作目录读取 `configs/config.yaml` 和 `.env`。即使把二进制加入 `PATH`，也仍然需要在包含这些文件的工作目录中运行。

#### 方式二：从源码构建

需要 Go 1.25 或更高版本。

```bash
cp configs/config.example.yaml configs/config.yaml
./build.sh
./news-briefing --help
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

抓取相关配置示例：

```yaml
fetch:
  timeout: 30s
  retry_times: 3
  retry_wait_time: 200ms
```

说明：

- `timeout`：HTTP 抓取超时
- `retry_times`：新闻源和 Watch 页面的总抓取尝试次数
- `retry_wait_time`：抓取失败后的重试间隔

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
- `retry_times`：邮件发送总尝试次数
- `retry_wait_time`：邮件发送重试间隔
- `use_proxy`：是否让邮件发送走已配置的 SOCKS5 代理

`email.*` 只作用于邮件发送；新闻源和 Watch 抓取使用上面的 `fetch.*` 配置。

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
- AI CLI 命令与批处理 flags（默认直连 `codex exec` + 隔离的非交互 flags）

分类示例：

```yaml
sources:
  - name: Example AI Feed
    url: https://example.com/ai.rss
    type: rss
    category: AI/科技
    source_role: original
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
  - name: Protected RSSHub Feed
    url: https://rsshub.example.com/example/route
    type: rss
    category: 新闻财经
    rsshub_access_key_env: RSSHUB_ACCESS_KEY
```

说明：

- `category` 可以自定义，不限于内置枚举
- `source_role` 可选 `primary`（官方/一手）、`original`（原创媒体）、`radar`（线索发现）和 `repost`（转载）；未配置时会按来源类型兼容推断
- 分组展示顺序按 `sources` 中首次出现顺序决定
- 如果运行时出现了配置里没有的分类，会追加到已配置分类之后
- 远程 RSSHub 必须使用 HTTPS；设置 `rsshub_access_key_env` 后，主密钥只放在 `.env`，请求仅携带按路由生成的访问码

简报优先把同一事件的一手来源与原创媒体合并核验，并由程序用真实文章 URL 生成邮件中的可点击引用。XHS 卡片只保留紧凑来源名，不渲染长 URL。

新来源可以先用 shadow 模式观察，不进入摘要、邮件、XHS、已读状态或来源健康告警：

```yaml
source_shadow:
  enabled: true
  retention: 72h
  timeout: 2m
  sources:
    - name: Example Shadow Feed
      url: https://example.com/shadow.rss
      type: rss
      category: AI/科技
      source_role: original
      max_items: 30
```

每个正式调度窗口的观察结果写入 `<output.dir>/state/source-shadow`，超过 `retention` 自动清理。建议连续观察 3 天后再人工决定是否把来源移入正式 `sources`。

程序默认只读取 `configs/config.yaml`。

### 3. AI CLI

确保 `configs/config.yaml` 中配置的 AI CLI 可用并已登录。项目通过本机已登录的 AI CLI 调用大模型，不依赖在本项目里额外配置 API Key。默认模板使用：

```yaml
ai:
  command: codex
  args:
    - exec
    - --ignore-user-config
    - --ephemeral
    - --skip-git-repo-check
    - --sandbox
    - read-only
    - --color
    - never
    - --disable
    - apps
    - --disable
    - plugins
    - --disable
    - remote_plugin
  models:
    default: gpt-5.6-terra
    default_effort: medium
    summary_editor: gpt-5.6-terra
    summary_editor_effort: high
    translation: gpt-5.3-codex-spark
    translation_effort: high
  summary:
    parallel_by_category: true
    max_concurrency: 3
    editor:
      enabled: true
      min_stories: 12
      target_stories: 17
      max_stories: 20
  append_system_prompt: true
```

runner 会通过 stdin 把每次 prompt 交给 `codex exec`，并自动追加输入标记 `-`。不要把 `-p`、`--model` 或末尾的 `-` 写进 `args`：Codex 的 `-p` 表示配置 profile；分类摘要与深挖使用 `models.default`，跨分类终审使用 `models.summary_editor`，翻译使用 `models.translation`，并分别应用对应的 effort。启用 `summary.parallel_by_category` 后，各分类会在 `max_concurrency` 限制下并行生成候选；启用 `summary.editor` 后，终审模型只按稳定 ID 选择、去重和排序候选，不会重写分类摘要中的事实。`min_stories`、`target_stories`、`max_stories` 是全局动态范围，不是固定的分类配额。今日速览会按分类动态输出 1-6 条要点；缺失或空分组会从已选新闻标题补齐，超过 6 条时保留前 6 条，不会仅因可恢复的数量偏差启动整轮 fallback。启用 `append_system_prompt` 时，runner 会把无人值守批处理指令映射为本次运行的 `developer_instructions`。

`--ignore-user-config` 与三个 feature disable 会让无人值守批处理与个人 MCP、apps 和 plugins 隔离；它们不会修改用户的 `~/.codex/config.toml`，也不会影响 Codex App 的 Computer Use 等功能。

### 4. output 输出配置

示例：

```yaml
output:
  dir: output
  mode: translated_only
  include_filtered_articles: false
```

`mode` 允许值：

- `original_only`：只显示原文/原始文章整理块
- `translated_only`：只显示 AI 生成的中文内容块
- `bilingual_translated_first`：先显示中文，再显示原文
- `bilingual_original_first`：先显示原文，再显示中文

`include_filtered_articles` 默认关闭。设为 `true` 后，`run`、`regen` 和 `serve` 会把时间窗口内但未命中关键词的候选新闻以中文译文附录追加到简报末尾；这些候选新闻不会进入 AI 摘要，也不会写入已读状态。

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
./news-briefing --help
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
./news-briefing regen --from "2026-03-18 08:00" --to "2026-03-18 14:00" --publish --replace-output
```

规则：

- `--from` / `--to` 必填，按 `schedule_timezone` 解析；未配置时使用系统本地时区，格式 `YYYY-MM-DD HH:MM`
- `--to` 必须晚于或等于 `--from`
- `--period` 可选；不传时默认取 `--to` 的 `HHMM`
- `--ignore-seen` 会跳过持久化已读状态，仅做当前批次内去重
- `--send-email` 默认关闭，显式传入时才发邮件
- `regen` 默认不运行 `publish_hook`，显式传入 `--publish` 才发布
- `regen` 默认把 Markdown 和卡片写入 `output.dir/manual/<运行时间>-<period>`，避免覆盖正式调度产物；只有显式传入 `--replace-output` 才写回正式输出目录

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
./news-briefing resend-md --file output/26.04.13-晚间-1800.md --email-recipient-match personal
```

规则：

- `--file` 必填
- 文件必须是 `.md`
- 文件路径必须位于 `output.dir` 下
- 收件人默认使用当前邮件配置；`--email-recipient-match` 可从已配置收件人中唯一匹配一个地址
- 邮件主题从 Markdown 文件名推导，例如 `26.04.13-晚间-1800.md` 会发成 `国际资讯简报 26.04.13 晚间 18:00`

### `carryover`

把已存在于 X visible 当前快照或历史归档中的重要帖子，一次性补入未来某个正式窗口：

```bash
./news-briefing carryover add \
  --url "https://x.com/example/status/123" \
  --target "2026-08-18 08:00"
./news-briefing carryover list
./news-briefing carryover remove --id <id>
```

- `add` 只登记状态，不立即生成、发邮件或发布 XHS；`target` 必须是未来时间。
- URL 必须能从配置允许的 X 账号当前快照或历史归档中解析，程序会保存文章快照及真实发布时间。
- 同一 URL 和窗口幂等去重；每个窗口最多 5 条。
- 正式任务会绕过该条目的原发布时间窗口和历史 `seen`，但仍保留真实发布时间，并要求分类编辑和最终终审必须引用。
- Markdown、邮件、启用的发布钩子及已读状态全部成功后才标记 `consumed`；失败时保持 `pending`，供同窗口恢复任务重试。
- 未消费条目在目标窗口 24 小时后显示为 `expired`；终态记录保留 14 天。
- 状态文件为 `<output.dir>/state/carryover.json`，使用原子写和跨进程锁。

### `serve`

守护模式，按 `configs/config.yaml` 中的 `schedule` 自动执行，与 `run` 使用同一输出链路。

模板配置当前示例：

```yaml
schedule:
  - "0 8 * * *"
  - "0 18 * * *"
schedule_delay: 10m
schedule_prefetch_wait_timeout: 2m
```

`serve` 的抓取窗口是按“当前触发时间”向前回推到“当前 `schedule` 中上一个计划时间点”计算的。`schedule_delay: 10m` 会把实际执行锚定在约 08:10 / 18:10；如果 cron 晚触发，只等待剩余时间，超过目标时间则立即执行。窗口边界仍保持在 08:00 / 18:00，便于上游本地抓取先落地数据。

在 08:00 / 18:00 准点触发时，`serve` 会让普通 RSS、Watch 抓取与外部 X 刷新并行执行，并把非 X 结果原子写入 `output/state/briefing-prefetch/`。X 就绪后，正式流程会校验快照版本、精确窗口和配置指纹，最多等待 `schedule_prefetch_wait_timeout`（默认 2 分钟），随后只读取 X 结果并合并。预取缺失、过期、失败、不匹配或等待超时时，会自动回退到原有完整抓取流程；预取本身不会发送邮件或触发发布钩子，超过 72 小时的快照会自动清理。

服务实际观察到 08:00 / 18:00 触发时，会把该窗口登记到唯一的长期状态文件 `output/state/briefing-scheduler.json`。只有处于 `pending`、`waiting_x` 或 `running` 的已登记窗口才有 watcher；默认每 1 分钟检查一次，进入 `done` / `failed` 后立即停止。若同窗口的 X 状态仍为 `running` 且 heartbeat 新鲜，窗口切到 `waiting_x`；X 进入终态或 heartbeat 超过 3 分钟未更新时，watcher 接管执行。简报自身也每分钟更新 heartbeat，超过 3 分钟可由重启后的 watcher 接管。cron、X 回调和 watcher 通过短期文件锁原子竞争同一窗口的 lease，所以同一窗口只会有一个有效执行者；旧 lease 不能覆盖接管后的状态。邮件成功时间也保存在同一记录中，接管时不会重复发送已经确认成功的邮件。短期 `.lock` 和原子写临时文件只在更新状态时存在，不是长期 marker。

AI attempt 失败时，调度状态会持久化白名单字段，例如 `ai_primary_error_stage=final_editor`、`ai_primary_error_code=overview_invalid`、受影响分类和耗时；fallback 成功后同时记录 `ai_recovered=true` 与恢复层级。状态和告警都不保存 Prompt、文章正文、完整 stderr、堆栈、token 或连接信息。

`output.xhs_preselection` 是可选的 XHS 专用前置筛选。启用后，邮件和 Markdown 仍使用 final editor 的原始 `stories`；card manifest 会优先采用其中符合来源规则的故事，再从分类 worker 的完整候选中按配置分类轮流补位到 `target_items`。企业重大负面、私营公司财务、融资估值、并购、IPO 条款和巨额担保必须有官方来源；普通产品与技术更新可保留单一来源。该流程不增加 AI 调用，`content-publisher` 仍保留最终硬门。

Watch 默认走“索引快检 + 正文深检”：索引新增或变化会立即读取正文；未变化文章按 `watch.deep_verify_interval` 到期后，以 `watch.deep_verify_batch_size` 为上限按最旧检查时间轮转。这样仍能发现 URL、标题和摘要均未变化时的正文静默更新，同时避免每个简报窗口下载全部历史正文。

RSS 源会在 `<output.dir>/state/rss-cache` 保存压缩响应和 ETag/Last-Modified 元数据。服务端返回 `304 Not Modified` 时复用已缓存 Feed；每份 `.source-stats.json` 同时记录各来源的抓取耗时、响应字节数、缓存状态、进入 AI 的数量和最终入选数量，便于识别大 Feed、低有效率来源及 AI 编辑阶段的损耗。

结构化简报只把最终 story 通过 `source_article_ids` 实际引用的主新闻写入 `seen.json`。通过关键词但被分类编辑或最终编辑舍弃的候选不会被提前标记已读，可在后续仍覆盖其发布时间的窗口中再次竞争；Watch 使用自己的状态库，不受此规则影响。

注意：`serve` 启动时只恢复上述状态文件里尚未结束的窗口，不会推算或补跑服务完全错过的历史触发点。例如 07:50 停服、08:01 启动时不会自动创建 08:00 窗口，按需使用 `regen` 手动补跑。

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

如果使用 macOS `launchd` 托管服务，可以改用下面的脚本；这些脚本默认使用 `~/Library/LaunchAgents/com.news-briefing.briefing.plist` 和 Label `com.news-briefing.briefing`：

```bash
./launch-start.sh
./launch-stop.sh
./launch-restart.sh
./launch-status.sh
```

### 方式二：macOS launchd

`launchd` 可以托管常驻的 `./news-briefing serve` 进程，实际触发时间仍以 `configs/config.yaml` 中的 `schedule` 为准。

建议创建 `~/Library/LaunchAgents/com.news-briefing.briefing.plist`，并让它直接执行当前仓库里的 `./news-briefing serve`。由于程序会从当前工作目录读取 `configs/config.yaml` 和 `.env`，plist 里要显式设置 `WorkingDirectory`。

最小示例：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.news-briefing.briefing</string>

  <key>WorkingDirectory</key>
  <string>/绝对路径/news-briefing</string>

  <key>ProgramArguments</key>
  <array>
    <string>/绝对路径/news-briefing/news-briefing</string>
    <string>serve</string>
  </array>

  <key>RunAtLoad</key>
  <true/>

  <key>KeepAlive</key>
  <true/>

  <key>StandardOutPath</key>
  <string>/绝对路径/news-briefing/logs/out.log</string>

  <key>StandardErrorPath</key>
  <string>/绝对路径/news-briefing/logs/err.log</string>
</dict>
</plist>
```

常用命令：

```bash
mkdir -p logs
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.news-briefing.briefing.plist 2>/dev/null || true
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.news-briefing.briefing.plist
launchctl kickstart -k gui/$(id -u)/com.news-briefing.briefing
launchctl print gui/$(id -u)/com.news-briefing.briefing
```

### 可选：为定时自动化配置本地 macOS 签名

如果每次重新构建 `./news-briefing` 后，macOS 都反复询问 Automation、Accessibility 或浏览器控制权限，可以用稳定的本地 Code Signing 证书签名本机二进制。

这是可选的本机配置。仓库不会包含证书、私钥、个人 Keychain 路径或签名密钥，公开 release 产物也不会因此改变。

1. 打开“钥匙串访问”。
2. 选择 Certificate Assistant > Create a Certificate。
3. 命名为例如 `News Briefing Local Code Signing`。
4. Certificate Type 选择 `Code Signing`，并保存到登录钥匙串。
5. 如果 macOS 提示信任设置，允许该证书用于代码签名。
6. 使用本地身份构建：

```bash
NEWS_BRIEFING_CODESIGN_IDENTITY="News Briefing Local Code Signing" ./build.sh
```

如果希望定时任务使用的本机二进制必须签名，可以启用严格模式：

```bash
NEWS_BRIEFING_CODESIGN_IDENTITY="News Briefing Local Code Signing" \
NEWS_BRIEFING_CODESIGN_REQUIRED=1 \
./build.sh
```

首次签名构建后，重启 launchd，并在 macOS 弹窗中允许一次：

```bash
./launch-restart.sh
```

停止并卸载：

```bash
launchctl bootout gui/$(id -u) ~/Library/LaunchAgents/com.news-briefing.briefing.plist
```

## 开发 / 测试

```bash
go test ./...
./build.sh
```

## License

本项目使用 MIT License，详见 [LICENSE](./LICENSE)。
