# TestDesk

桌面端 AI 浏览器测试录制工具。用自然语言描述测试目标，AI 驱动浏览器完成操作并生成 Python 或 JavaScript 脚本，脚本保存后可以反复执行，回放时不再调用 AI。

技术栈：Wails v2（Go）+ React + Vite + Tailwind CSS + shadcn 风格组件。AI 接入使用 OpenAI 兼容 API（官方 `openai-go` SDK）。浏览器驱动使用 Playwright（Node 侧车）。

设计文档见 [docs/recording-design.md](docs/recording-design.md)。

## 目录结构

```
app.go                     Wails 绑定：设置、运行环境、脚本 CRUD、运行、录制
main.go
internal/
  settings/                设置存储（%AppData%/testdesk/settings.json）
  runtimeenv/              运行环境：状态检查、安装流程，内嵌 sidecar/server.mjs
  browser/                 侧车进程客户端（JSON 行协议）
  recorder/                录制会话状态机 + LLMAgent（OpenAI 兼容工具调用循环）
  codegen/                 动作日志 → Python / JavaScript 脚本
  runner/                  回放：执行 Python 或 JS 脚本，流式输出
  scripts/                 脚本模型与 JSON 存储
  proc/                    子进程辅助（Windows 下隐藏控制台窗口）
frontend/src/
  components/ui/           shadcn 风格基础组件
  components/              侧边栏
  pages/                   录制页、脚本库页、设置页
  lib/api.ts               Wails 绑定与事件订阅的统一入口
```

## 运行环境

应用依赖系统中的 **Node.js 18 或更高版本**。Python 可选，只影响 Python 脚本的回放。

首次使用：打开「设置」→「运行环境」→「安装运行环境」。安装会做这些事：

1. 检查 Node.js 版本
2. 在 `%AppData%/testdesk/runtime` 下安装 `playwright` 与 `@playwright/test`
3. 写入侧车脚本
4. 下载 Chromium（首次较慢，需要网络）
5. 若检测到 Python，创建 venv 并安装 Python 版 Playwright

Node 或 npm 不在 PATH 中时，在设置里填写 node 的完整路径。

## 开发

前提：Go 1.25+、Node 18+、Wails CLI v2。

```bash
wails dev
```

## 测试

```bash
go test ./...
```

默认只跑单元测试。需要真实环境的集成测试通过环境变量开启：

```bash
# 1. 安装运行环境到指定目录（需要网络）
TESTDESK_INTEGRATION=1 TESTDESK_RUNTIME_DIR=/path/to/runtime go test ./internal/runtimeenv -run TestInstallIntegration -v

# 2. 启动真实 Chromium 测试侧车
TESTDESK_RUNTIME_DIR=/path/to/runtime go test ./internal/browser -run TestSidecarIntegration -v

# 3. 端到端：模拟 OpenAI 兼容服务录制，生成 JS 并回放
TESTDESK_RUNTIME_DIR=/path/to/runtime go test ./internal/recorder -run TestRecordAndReplayIntegration -v
```

第 3 步回放时 Chromium 会以有界面的方式打开一次。

## AI 接入

在「设置」中填写：

- **Base URL**：兼容 OpenAI 的 API 地址，例如 `https://api.openai.com/v1`
- **API Key**
- **模型名称**：所用服务商提供的模型 ID

API Key 以明文保存在 `settings.json` 中，请注意保管该文件。

## 事件协议

| 事件 | 载荷 | 说明 |
| --- | --- | --- |
| `recorder:event` | `{kind, text}` | kind：status / message / action / code / error / done |
| `run:output` | `{runId, stream, line}` | 脚本输出，stream：stdout / stderr |
| `run:done` | `{runId, scriptId, result}` | 执行结束，result 含 exitCode、durationMs、error |
| `setup:progress` | `{step, status, message}` | 运行环境安装进度，status：running / done / failed / skipped / log |

## 已知限制

- Python 脚本的回放依赖 Python 运行环境，未安装时该步骤会被跳过。
- 录制只支持单个会话。
- 录制生成的脚本以 `headless: false` 运行，便于观察。
- 尚未做 AI 修复失败脚本、回放历史、并行执行。
