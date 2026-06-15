# Agent Light

Agent Light 是一个面向编程工具状态灯的统一方案。它接收 Codex、Claude Code、Antigravity 的 hooks 事件，转换成统一状态，上报到本地或远程 server，再由 ESP32-C3 通过 Wi-Fi 获取状态并驱动红黄绿三色灯。

本文已经合并原来的安装手册、事件映射和项目规划内容，是当前项目主文档。

## 目录

1. [目标灯效](#目标灯效)
2. [系统架构](#系统架构)
3. [项目结构](#项目结构)
4. [首次使用](#首次使用)
5. [安装方式](#安装方式)
6. [三家 hooks 配置](#三家-hooks-配置)
7. [统一事件与 API](#统一事件与-api)
8. [状态映射](#状态映射)
9. [运行配置](#运行配置)
10. [本地测试](#本地测试)
11. [故障排查](#故障排查)
12. [项目规划](#项目规划)
13. [参考文档](#参考文档)

## 目标灯效

最终只关注 4 个统一状态：

| 状态 | code | 灯效 | 含义 |
| --- | --- | --- | --- |
| 空闲 | `idle` | 绿灯常亮 | 当前没有任务，或本轮任务结束 |
| 思考 | `thinking` | 黄灯呼吸 / 慢闪 | AI 正在分析、生成、整理回复 |
| 忙碌 | `busy` | 红灯常亮 | AI 正在执行命令、改文件、调用工具 |
| 需要审批 | `approval` | 红灯快闪 | 需要你参与，通常是等待授权或确认 |

核心原则：

```text
红灯快闪 = 需要你参与
红灯常亮 = AI 正在动手
黄灯 = AI 正在思考
绿灯 = 空闲
```

## 系统架构

```text
Codex hooks
Claude Code hooks
Antigravity hooks
        |
        v
collector 数据收集端
        |
        | POST /api/devices/:deviceId/events
        v
server 状态处理端
        |
        | GET /api/devices/:deviceId/status
        v
ESP32-C3 固件
        |
        v
红 / 黄 / 绿 LED
```

当前实现按 device-id 分区，每个 device（用户/灯）一份独立状态：

```text
collector 上报到 /api/devices/<deviceId>/events -> 覆盖该 deviceId 的状态
不同 deviceId 互不影响（多用户/多灯各自独立）
超过 AGENT_LIGHT_IDLE_TTL_MS 未更新 -> 该 deviceId 回落 idle/绿灯
```

每个 deviceId 内部是 `last-write-wins + TTL`。同一用户名下所有 collector 填同一个 `DEVICE_ID`（共享一盏灯），不同用户填不同的（各亮各的）。

## 项目结构

当前可运行原型：

```text
agent_light/
  collector/
    claude-code/claude-hook.js
    codex/codex-hook.js
    antigravity/antigravity-hook.js
  server/
    main.go
    daemon.go
    build.sh
    dev-server.js  (旧 Node 开发参考)
```

推荐长期目录结构：

```text
agent_light/
  packages/shared/
    src/states.ts
    src/event-schema.ts
    src/light-effects.ts
  collector/
    shared/
    codex/
    claude-code/
    antigravity/
  server/
    main.go
    daemon.go
    build.sh
    README.md
  firmware/
    esp32-c3-agent-light/
  scripts/
    install-global-hooks.ts
    watch-status.ts
```

技术选型：

| 层 | 推荐技术 | 职责 |
| --- | --- | --- |
| collector | Node.js / TypeScript | 读取 hooks stdin，映射统一事件，上报 server |
| server | Go 标准库 HTTP | 鉴权、保存当前状态、提供设备查询接口、后台守护运行 |
| firmware | PlatformIO / Arduino C++ | ESP32-C3 Wi-Fi 轮询状态并点灯 |

当前 server 主实现是 Go 版，接口兼容旧的 `server/dev-server.js`。

## 首次使用

### 1. 前置条件

| 项 | 要求 |
| --- | --- |
| Node.js | 18+，collector 使用全局 `fetch` |
| Go | 1.22+，server 编译使用 |
| 编程工具 | Claude Code / Codex / Antigravity 任一 |
| 设备 | ESP32-C3 + 红黄绿 LED；没有设备也能用 curl 看状态 |

验证 Node / Go：

```bash
node --version
go version
```

### 2. 启动 server

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
go run .
```

看到：

```text
Agent Light server listening on http://127.0.0.1:4318
Idle TTL: 1200s (超时未更新 -> 绿灯)
```

验证：

```bash
curl http://127.0.0.1:4318/health
```

编译后也可以后台运行：

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
./build.sh darwin-arm64
./build/darwin-arm64/agent-light-server server start
./build/darwin-arm64/agent-light-server server status
./build/darwin-arm64/agent-light-server server stop
```

Go 服务端的构建脚本是 [`server/build.sh`](server/build.sh)。常用命令：

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
./build.sh all            # mac arm64 + linux x64
./build.sh darwin-arm64   # Apple Silicon macOS
./build.sh linux-amd64    # Linux x64
./build.sh linux-x64      # Linux x64 别名
```

输出位置：

```text
server/build/darwin-arm64/agent-light-server
server/build/linux-amd64/agent-light-server
server/dist/agent-light-server-darwin-arm64
server/dist/agent-light-server-linux-amd64
```

注意：Go 版服务端如果没有指定 token，每次启动都会随机生成 `Collector token` 和 `Device token`。后台启动时会打印一次，之后也可以通过 `server status` 查看当前运行中的 token：

```bash
./build/darwin-arm64/agent-light-server server status
```

collector 的 `AGENT_LIGHT_COLLECTOR_TOKEN`、设备查询的 `AGENT_LIGHT_DEVICE_TOKEN` 必须使用当前运行中的值。推荐用命令参数固定 token：

```bash
./build/darwin-arm64/agent-light-server \
  --collector-token your-collector-token \
  --device-token your-device-token \
  server start
```

也可以同时指定监听地址、端口、TTL 和每个 deviceId 的事件保留数量：

```bash
./build/darwin-arm64/agent-light-server \
  --host 0.0.0.0 \
  --port 4318 \
  --collector-token your-collector-token \
  --device-token your-device-token \
  --idle-ttl-ms 1200000 \
  --max-recent-events 100 \
  server start
```

服务端完整使用方法见 [`server/README.md`](server/README.md)。

CI/CD 自动构建见 [`BUILD.md`](BUILD.md)。推送 `v*` tag 会自动构建 `darwin-arm64` 和 `linux-amd64`，并把带平台后缀的二进制文件附加到 GitHub Release：

```bash
git tag v0.1.0
git push origin v0.1.0
```

### 3. 装全局 hooks

| 工具 | 全局配置文件 |
| --- | --- |
| Claude Code | `~/.claude/settings.json` |
| Codex | `~/.codex/hooks.json` |
| Antigravity | `~/.gemini/config/hooks.json` |

安装原则：

```text
不要把 collector 文件复制到三家工具目录
不要整份覆盖已有全局配置文件
只把本文给出的 hooks 片段合并到对应配置文件
hooks 里的 command 指向本项目 collector 脚本的绝对路径
```

真正运行的脚本仍留在：

```text
/Users/apple/user/VscodeProject/agent_light/collector/claude-code/claude-hook.js
/Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js
/Users/apple/user/VscodeProject/agent_light/collector/antigravity/antigravity-hook.js
```

三家工具的全局配置文件只负责登记“某个 hook 事件发生时，调用哪个脚本”。

### 4. 验证上报

用任一工具做个动作，然后查状态：

```bash
curl -s -H 'Authorization: Bearer <device-token>' \
  http://127.0.0.1:4318/api/devices/desk-light-01/status
```

应看到 `state` / `color` / `effect` 随活动变化。

## 安装方式

三家工具都采用同一个思路：

| 内容 | 放哪里 |
| --- | --- |
| collector 脚本 | 留在本项目 `collector/<tool>/...` |
| hooks 配置 | 合并到工具自己的全局配置文件 |
| server 地址/token | 配在 collector 脚本顶部常量；也可用进程环境变量临时覆盖，但不要写进 Claude Code settings |
| 状态结果 | collector POST 到 server；server 内存保存最新状态，设备通过 HTTP 查询 |

不要把 `collector/` 目录复制到 `~/.claude`、`~/.codex` 或 `~/.gemini`。这些目录里只放配置。

如果配置文件已经存在：

```text
保留已有 permissions / 其他 hooks
只新增或更新 Agent Light 相关 hook 项
不要直接用本文 JSON 整文件覆盖
```

如果项目搬家，唯一要改的是三家配置里的脚本绝对路径。

如果 server 没启动，collector 会在 POST 超时或失败后正常退出，不影响 Claude / Codex / Antigravity 正常使用；只是状态灯不会更新。默认超时由 `AGENT_LIGHT_POST_TIMEOUT_MS` 控制，当前是 800ms。

## 三家 hooks 配置

全局 hooks 的安装顺序建议固定成这样：

```text
1. 确认本项目绝对路径
2. 确认远程 server 地址、device id、collector token
3. 把对应 hooks 片段合并到工具的全局配置文件
4. 确认 collector 脚本里的 server 地址、device id、collector token 正确；临时覆盖时才用进程环境变量
5. 触发一个新会话或工具调用
6. 用 /status 看当前灯态，用 /events 看最近事件
```

如果三家工具都在同一台机器上跑，建议统一使用同一个 device id，例如 `desk-light-01`。这样 Claude Code、Codex、Antigravity 会共同驱动同一盏灯；如果希望分开显示，就给每个工具配置不同 `AGENT_LIGHT_DEVICE_ID`。

### Claude Code

| 项 | 值 |
| --- | --- |
| 配置文件 | `~/.claude/settings.json` |
| 字段 | 顶层 `hooks` |
| CLI / Desktop | 优先共用 `~/.claude/settings.json`；Desktop 若不读取 CLI settings，把同一段 hooks 抄到 Desktop 自己的 settings |
| 默认接入事件 | 全量接入官方 30 个生命周期事件，见 `collector/claude-code/settings.example.json` |
| 异步 | 支持 `async: true`，本项目使用它减少等待 |
| matcher | 按官方 matcher 能力配置；`FileChanged` 需要写具体文件名，不是通配符 |

当前 Claude Code 官方生命周期事件已经全部接入：

```text
SessionStart, Setup, UserPromptSubmit, UserPromptExpansion,
PreToolUse, PermissionRequest, PermissionDenied,
PostToolUse, PostToolUseFailure, PostToolBatch,
Notification, MessageDisplay, SubagentStart, SubagentStop,
TaskCreated, TaskCompleted, Stop, StopFailure, TeammateIdle,
InstructionsLoaded, ConfigChange, CwdChanged, FileChanged,
WorktreeCreate, WorktreeRemove, PreCompact, PostCompact,
Elicitation, ElicitationResult, SessionEnd
```

全量配置以 [`collector/claude-code/settings.example.json`](collector/claude-code/settings.example.json) 为准。若你只想让灯更安静，可以只保留核心事件：`SessionStart`、`UserPromptSubmit`、`PreToolUse`、`PostToolUse`、`PostToolUseFailure`、`PermissionRequest`、`Notification`、`Stop`、`StopFailure`、`SessionEnd`。

具体步骤：

```text
1. 打开 ~/.claude/settings.json
2. 如果文件不存在，创建一个 JSON 对象：{}
3. 保留已有 permissions、hooks 等配置
4. 把 collector/claude-code/settings.example.json 里的 hooks 合并到顶层 hooks 字段
5. 把 /absolute/path/to/agent_light/collector/claude-code/claude-hook.js 改成本项目真实绝对路径
6. 如果保留 FileChanged，把 matcher 改成你要监听的具体文件名列表，例如 CLAUDE.md|settings.json|.env|.envrc
7. 在 Claude Code 里运行 /hooks，确认事件数和命令都能看到
8. 触发一个 Claude Code 新会话或工具调用
9. 查询 /api/devices/:deviceId/events?limit=20&details=1 验证事件到达
```

Claude Code 会热加载 settings 文件，通常无需重启。全量接入会记录更多细状态，但 `SubagentStop`、`MessageDisplay`、`FileChanged`、`CwdChanged` 这类事件可能比较高频；如果只关注最终空闲，可以从全局 hooks 里删掉它们。


### Codex

| 项 | 值 |
| --- | --- |
| 配置文件 | `~/.codex/hooks.json`，或 `~/.codex/config.toml` 内联 `[hooks]` |
| 字段 | 顶层 `hooks` |
| 官方事件 | `SessionStart`、`SubagentStart`、`PreToolUse`、`PermissionRequest`、`PostToolUse`、`PreCompact`、`PostCompact`、`UserPromptSubmit`、`SubagentStop`、`Stop` |
| 默认接入事件 | 全量接入官方 10 个事件 |
| 异步 | Codex 不支持 async command hooks；带 `async:true` 的 handler 会被跳过 |
| matcher | 支持 matcher 的事件统一用 `"*"`，避免不同 Codex 版本的 source/trigger/tool 名差异导致漏采；`UserPromptSubmit` / `Stop` 不支持 matcher |
| 超时 | `SessionStart` 最容易在启动阶段丢包，collector 上报超时别设太小 |
| 信任 | 加完配置后在 Codex 里运行 `/hooks` 审核并 trust |

具体步骤：

```text
1. 打开 ~/.codex/hooks.json
2. 如果文件不存在，创建：{"hooks":{}}
3. 只合并下面片段里的 hooks，不要覆盖其他 hook 来源
4. 如果用 cc-switch 切换 Codex 配置，把 hooks.state 信任记录放到 cc-switch 的 Codex 通用 config.toml
5. 在 Codex 里运行 /hooks
6. trust 新增或 changed 的 hook
7. 新开或 resume 一个会话验证 SessionStart；普通提问可验证 UserPromptSubmit / Stop
8. 查询 /api/devices/:deviceId/events?limit=20&details=1 验证事件到达
```

写到 `~/.codex/hooks.json`。如果文件已存在，只合并 `hooks`：

```json
{
  "hooks": {
    "SessionStart": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "SubagentStart": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "UserPromptSubmit": [
      { "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "PreToolUse": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "PermissionRequest": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "PostToolUse": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "PreCompact": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "PostCompact": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "SubagentStop": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ],
    "Stop": [
      { "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/codex/codex-hook.js" } ] }
    ]
  }
}
```

Codex 用 shell-form `command` 字符串，不用 Claude 那种 `args`。配置或命令变化后，需要重新 `/hooks` trust。

Codex 的信任状态不在 `hooks.json`，而在 `~/.codex/config.toml` 的 `[hooks.state]`。如果你用 cc-switch，它可能重写 `~/.codex/config.toml`，所以要把已 trust 的 `[hooks.state]` 一并放到 cc-switch 的 Codex 通用配置里，否则每次切换 provider 后可能又要手动 trust。

cc-switch 的 Codex 通用配置里至少要保留类似下面这段：

```toml
[hooks.state]

[hooks.state."/Users/apple/.codex/hooks.json:session_start:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:subagent_start:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:user_prompt_submit:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:pre_tool_use:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:permission_request:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:post_tool_use:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:pre_compact:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:post_compact:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:subagent_stop:0:0"]
trusted_hash = "sha256:..."

[hooks.state."/Users/apple/.codex/hooks.json:stop:0:0"]
trusted_hash = "sha256:..."
```

不要手写 hash。正确做法是先在 Codex `/hooks` 里 trust 一次，然后把 `~/.codex/config.toml` 里生成的 `[hooks.state]` 原样复制到 cc-switch 的 Codex 通用 `config.toml`。以后只要 `~/.codex/hooks.json` 里的 hook 定义不变，这些 hash 就能复用。

### Antigravity

| 项 | 值 |
| --- | --- |
| 配置文件 | `~/.gemini/config/hooks.json`，或项目级 `<workspace>/.agents/hooks.json` |
| 结构 | 顶层是命名组，如 `agent-light`；组内再配事件 |
| 默认接入事件 | `PreInvocation`、`PostInvocation`、`PreToolUse`、`PostToolUse`、`Stop` |
| 事件名传递 | 作为 argv 传给脚本，例如 `antigravity-hook.js PreToolUse` |
| 审批 | collector 往 stdout 输出 `decision` JSON，Antigravity 据此决定是否弹审批 |

具体步骤：

```text
1. 打开 ~/.gemini/config/hooks.json
2. 如果文件不存在，创建一个 JSON 对象：{}
3. 新增或更新顶层命名组 agent-light
4. 把下面片段写进 agent-light 组
5. 把脚本路径改成本项目真实绝对路径
6. 重启或重新打开 Antigravity 会话
7. 查询 /api/devices/:deviceId/events?limit=20&details=1 验证事件到达
```

写到 `~/.gemini/config/hooks.json`。如果文件已存在，只新增或更新 `agent-light` 这个命名组：

```json
{
  "agent-light": {
    "PreInvocation": [
      { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/antigravity/antigravity-hook.js PreInvocation" }
    ],
    "PostInvocation": [
      { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/antigravity/antigravity-hook.js PostInvocation" }
    ],
    "PreToolUse": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/antigravity/antigravity-hook.js PreToolUse" } ] }
    ],
    "PostToolUse": [
      { "matcher": "*", "hooks": [ { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/antigravity/antigravity-hook.js PostToolUse" } ] }
    ],
    "Stop": [
      { "type": "command", "command": "node /Users/apple/user/VscodeProject/agent_light/collector/antigravity/antigravity-hook.js Stop" }
    ]
  }
}
```

Antigravity 的 `PreToolUse` / `PostToolUse` 支持 `matcher`。`PreInvocation` / `PostInvocation` / `Stop` 不用 matcher。

Antigravity hooks 的顶层不是 `{"hooks": ...}`，而是命名组结构：

```json
{
  "agent-light": {
    "PreInvocation": []
  }
}
```

这点和 Codex / Claude Code 不一样，写错结构会导致 hooks 不触发。

## 统一事件与 API

### 统一事件

collector 最终向 server 发送同一种事件：

```json
{
  "source": "codex",
  "state": "approval",
  "event": "PermissionRequest",
  "message": "Codex 需要审批",
  "details": {
    "toolName": "Bash"
  }
}
```

三个层面：

```text
state   粗态 idle/thinking/busy/approval，喂红绿灯
event   原始 hook 事件名，给程序判断精确生命周期
details 工具特有细节，如 toolInput、error、fullyIdle、terminationReason
```

server 只强校验 `state`，其他字段可选。时间戳由 server 盖。

### Collector 上报

```http
POST /api/devices/:deviceId/events
Authorization: Bearer <collector-token>
Content-Type: application/json
```

`:deviceId` 即 collector 配置里的 `DEVICE_ID`（每用户/每灯一个）。

### 设备查询

```http
GET /api/devices/:deviceId/status
Authorization: Bearer <device-token>
```

这是给 ESP32 用的轻量接口，默认不返回 `details`。响应示例：

```json
{
  "state": "approval",
  "color": "red",
  "effect": "fast_blink",
  "light": {
    "intent": "approval",
    "primary": "#EF4444",
    "secondary": "#F97316",
    "brightness": 190,
    "speed": 140,
    "density": 4,
    "priority": 90,
    "ttlMs": 1200000
  },
  "message": "Codex 需要审批",
  "source": "codex",
  "event": "PermissionRequest",
  "updatedAt": "2026-06-13 18:40:46"
}
```

多显示设备读取同一个状态通道时，可以追加 `displayId` 或 `layout`：

```http
GET /api/devices/:deviceId/status?displayId=desk-ring-12
GET /api/devices/:deviceId/status?displayId=desk-main&layout=matrix4x4
Authorization: Bearer <device-token>
```

响应会额外带上 `display`：

```json
{
  "state": "busy",
  "color": "red",
  "effect": "solid",
  "light": {
    "intent": "busy",
    "primary": "#3B82F6",
    "secondary": "#8B5CF6",
    "brightness": 120,
    "speed": 90,
    "density": 3,
    "priority": 30,
    "ttlMs": 1200000
  },
  "display": {
    "id": "desk-ring-12",
    "layout": "ring12",
    "pixels": 12,
    "description": "12 pixel ring"
  },
  "message": "Codex 正在动手",
  "source": "codex",
  "event": "PreToolUse",
  "updatedAt": "2026-06-15 14:30:00"
}
```

调试时需要查看细状态，可以加 `details=1`：

```http
GET /api/devices/:deviceId/status?details=1
Authorization: Bearer <device-token>
```

### 最近事件查询

```http
GET /api/devices/:deviceId/events?limit=20&details=1
Authorization: Bearer <device-token>
```

这是给调试用的事件回看接口。`/status` 只返回最后一个灯态；`/events` 会返回最近事件，适合确认 `SessionStart`、`PreToolUse`、`PermissionRequest` 这类瞬时事件是否到达服务端。

参数：

| 参数 | 说明 |
| --- | --- |
| `limit` | 返回最近多少条，默认 `20`，最大受 `AGENT_LIGHT_MAX_RECENT_EVENTS` 限制 |
| `details=1` | 返回完整 `details`；不传时只返回轻量事件信息 |

事件日志按 `deviceId` 独立保存。`desk-light-01`、`alice-light`、`bob-light` 都是不同的灯/用户，每个 `deviceId` 默认各保留最近 100 条事件，不会互相混在一起。

### 健康检查

```http
GET /health
```

## 显示设备与 AI 光效

### 状态通道与显示设备

hooks 只负责更新一个统一状态通道，物理灯只是订阅这个通道的不同显示终端：

```text
collector -> POST /api/devices/workspace/events

12 环形灯 -> GET /api/devices/workspace/status?displayId=desk-ring-12
8x8 方阵 -> GET /api/devices/workspace/status?displayId=desk-matrix-8x8
4x4 方阵 -> GET /api/devices/workspace/status?displayId=desk-matrix-4x4
2x2 方阵 -> GET /api/devices/workspace/status?displayId=mini-2x2
单个灯   -> GET /api/devices/workspace/status?displayId=single-dot
6 位条形 -> GET /api/devices/workspace/status?displayId=bar-6
```

这里 `workspace` 是状态通道，同一用户、同一桌面、同一组 agent 可以共用一个 `deviceId`。`displayId` 是具体显示设备，设备端固件根据自己的灯型渲染。

服务端保留旧字段 `color/effect`，供简单红黄绿灯使用；新设备优先读取 `light` 和 `display`。

### 支持的灯型

| layout | 灯型 | 像素数 | 推荐用途 |
| --- | --- | --- | --- |
| `pixel1` | 单个灯 | 1 | 最小状态指示，靠亮度曲线和颜色过渡表现质感 |
| `matrix2x2` | 2x2 方形 | 4 | 四象限脉冲、对角线呼吸、整体爆闪 |
| `matrix4x4` | 4x4 方形 | 16 | 中心扩散、低分辨率等离子、边框能量场、数据雨 |
| `matrix8x8` | 8x8 方形 | 64 | 主推荐显示形态，适合极光、数据雨、粒子、波纹、低分辨率 AI 核心 |
| `ring12` | 12 环形 | 12 | 旋转能量环、粒子追逐、黑洞吸入、完成扫圈 |
| `bar6` | 6 位条形 | 6 | 数据流、进度扫描、左右波、流星拖尾 |

`layout` 可以显式传入，也可以通过常见 `displayId` 自动推断：

```text
desk-ring-12       -> ring12
desk-matrix-8x8    -> matrix8x8
desk-matrix-4x4    -> matrix4x4
mini-2x2           -> matrix2x2
single-dot         -> pixel1
bar-6 / strip-6    -> bar6
```

### 光效字段

`light` 是服务端给设备端的视觉意图，不直接指定每颗灯珠颜色。设备端固件按自己的 `layout` 实现具体动画。

| 字段 | 说明 |
| --- | --- |
| `intent` | 当前语义状态，如 `idle`、`thinking`、`busy`、`approval` |
| `primary` | 主色，通常用于主体光流 |
| `secondary` | 辅色，通常用于拖尾、背景辉光或对比层 |
| `brightness` | 建议亮度，0-255，设备可按硬件限制再压低 |
| `speed` | 建议动画速度，0-255 |
| `density` | 粒子、亮点、扫描线密度 |
| `priority` | 状态优先级，设备端可用它决定是否打断当前动画 |
| `ttlMs` | 服务端状态 TTL，设备端可用它判断离线/过期降级 |

后续固件可以扩展更多字段：

| 字段 | 用途 |
| --- | --- |
| `animation` | 指定动画算法，如 `quantum_drift`、`data_stream` |
| `palette` | 指定调色板，如 `ai_nebula`、`ai_focus` |
| `accent` | 点缀色，适合白色星点、红橙核心、紫色边缘光 |
| `trail` | 拖尾长度或残影强度 |
| `glow` | 背景辉光强度 |
| `sparkle` | 星尘/粒子闪烁密度 |
| `direction` | 环形/条形流动方向，如 `cw`、`ccw`、`left`、`right` |

### AI 风格动画

目标不是普通红黄绿状态灯，而是“AI 正在流动”的桌面装置感：低亮、柔和、带拖尾、带层次，在需要用户注意时才明显提醒。

| 状态 | 动画名 | 氛围 | 推荐颜色 |
| --- | --- | --- | --- |
| `idle` | `aurora_core` | 青绿/蓝紫极光缓慢漂移，像在线但安静的核心 | `#22C55E` + `#14B8A6` + 暗蓝背景 |
| `SessionStart` | `neural_wake` | 白蓝光从中心或起点扩散，像神经网络被唤醒 | `#E0F2FE` + `#38BDF8` + `#A78BFA` |
| `thinking` | `quantum_drift` | 暖金核心 + 青蓝微粒慢速漂移，有呼吸和星尘 | `#FACC15` + `#38BDF8` + `#A78BFA` |
| `busy` | `data_stream` | 蓝紫高速数据流，带拖尾、扫描和轻微闪点 | `#3B82F6` + `#8B5CF6` + 柔白拖尾 |
| `approval` | `alert_gate` | 红橙能量门快速脉冲，中间夹白色闪点 | `#EF4444` + `#F97316` + 白色闪点 |
| `done` | `holo_bloom` | 绿色/青色扩散一圈，然后柔和回到 idle | `#22C55E` + `#2DD4BF` + 柔白 |
| `error` | `red_singularity` | 深红收缩/爆闪 2-3 次，然后暗红余辉 | `#DC2626` + `#7F1D1D` + `#FCA5A5` |
| `offline` | `cold_sleep` | 暗蓝灰单点慢闪，像休眠舱 | `#334155` + `#94A3B8` |

### 8x8 方阵主光效

8x8 WS2812 有 64 个像素，是当前最推荐作为主状态灯的形态。它比 4x4 更能表现流动和层次，又比 16x16 更省电、更容易供电和散热。建议加 2mm-3mm 乳白亚克力扩散板，灯珠到扩散面保持 8mm-15mm。

8x8 固件建议把像素抽象成二维坐标：

```text
(0,0) ... (7,0)
  .         .
(0,7) ... (7,7)
```

如果你的灯板是蛇形走线，固件里只在 `xy(x, y)` 映射函数处理真实灯珠编号，动画代码永远按二维坐标写。

| 状态 | 动画 | 8x8 表现 |
| --- | --- | --- |
| `idle` | `aurora_core` | 深蓝底色上叠加青绿/紫色低速噪声云，四角微亮，中心缓慢呼吸 |
| `SessionStart` | `neural_wake` | 中心 2x2 先亮白蓝，然后向外扩散一圈青色波纹，最后落到 idle |
| `thinking` | `quantum_drift` | 中心暖金核心缓慢呼吸，周围青蓝粒子随机漂移，偶尔紫色星点闪烁 |
| `busy` | `data_stream` | 蓝紫扫描线从左到右流动，局部白色高亮像数据包，旧像素用 fade 拖尾 |
| `approval` | `alert_gate` | 外圈红橙边框快速脉冲，中心 2x2 白点闪烁，强提醒但不长时间满亮 |
| `done` | `holo_bloom` | 中心绿色亮起后向四周扩散，边缘青色扫过一圈，然后回到 idle |
| `error` | `red_singularity` | 全屏暗红收缩到中心，再红白短闪 2-3 次，随后暗红余辉逐渐熄灭 |
| `offline` | `cold_sleep` | 只有左上或中心一点暗蓝灰慢闪，其余像素极低亮 |

推荐实现技巧：

| 技巧 | 说明 |
| --- | --- |
| `fadeToBlackBy` | 每帧整体衰减，形成自然拖尾 |
| `beatsin8` / `sin8` | 做呼吸、波动、中心扩散 |
| `inoise8` | 做极光、云雾、能量场 |
| 调色板 | 把 `ai_nebula`、`ai_focus`、`ai_work`、`ai_alert` 做成固定 palette |
| 低亮运行 | 常态亮度建议 40-110，只有 approval/error 短时间到 160-200 |

### 不同灯型的动画实现

同一个 `intent` 在不同灯型上应该保持同一气质，但动画算法可以完全不同。

| intent | `pixel1` | `matrix2x2` | `matrix4x4` | `matrix8x8` | `bar6` | `ring12` |
| --- | --- | --- | --- | --- | --- | --- |
| `idle` | 青绿低亮慢呼吸 | 四颗同步低亮呼吸 | 四角微亮 + 中心暗蓝漂移 | 极光噪声云 + 中心慢呼吸 | 中间两颗低亮呼吸 | 极光色慢速绕环 |
| `thinking` | 暖金呼吸 + 青色闪点 | 四象限顺序脉冲 | 中心向外扩散波纹 + 星点 | 暖金核心 + 青蓝粒子漂移 | 双向柔和来回流动 | 单点慢旋转 + 柔和拖尾 |
| `busy` | 蓝紫微闪 | 顺时针轮转 | 横向/纵向数据扫描线 | 蓝紫数据雨 + 白色数据包拖尾 | 蓝紫数据流向右滑动 | 三点追逐 + 紫色残影 |
| `approval` | 红橙快脉冲 | 全体红橙双闪 | 边框红橙闪烁 + 中心白点 | 红橙边框能量门 + 中心白闪 | 全条快闪 + 两端白点 | 红橙双向脉冲 |
| `done` | 绿色柔闪一次 | 由暗到亮再回落 | 中心绿色扩散 | 绿色中心 bloom + 青色边缘扫过 | 从左到右扫过 | 绿色扫一圈 |
| `offline` | 暗蓝灰慢闪 | 单角慢闪 | 单点慢闪 | 单点暗蓝灰休眠闪烁 | 第一颗慢闪 | 单点慢闪 |

设备端固件建议用 FastLED / Adafruit NeoPixel 实现。FastLED 更适合做调色板、噪声、拖尾和多层动画；Adafruit NeoPixel 更简单稳定，适合先把硬件跑通。

## 状态映射

### 统一状态模型

| state | 灯效 |
| --- | --- |
| `idle` | 绿灯常亮 |
| `thinking` | 黄灯呼吸 / 慢闪 |
| `busy` | 红灯常亮 |
| `approval` | 红灯快闪 |

映射原则：

```text
idle      = 当前没有需要关注的执行循环，或会话/回合已结束
thinking  = 模型正在分析、生成、压缩上下文，或工具结束后准备下一步
busy      = 正在执行工具、子任务、后台任务或会修改环境的操作
approval  = 明确需要用户介入审批、确认或输入
```

服务器状态模型：

```text
按 device-id 分区，每份 last-write-wins + TTL
```

超过 `AGENT_LIGHT_IDLE_TTL_MS` 默认 20 分钟未更新时，GET 状态会回落到 `idle`。`/status` 只表示当前灯态，采用 last-write-wins；要排查瞬时事件（例如 Codex `SessionStart`），请查 `/events?limit=20&details=1`。

### Codex

官方 hook 事件：`SessionStart`、`SubagentStart`、`PreToolUse`、`PermissionRequest`、`PostToolUse`、`PreCompact`、`PostCompact`、`UserPromptSubmit`、`SubagentStop`、`Stop`。

| Codex event | state | 灯效 | message | 说明 |
| --- | --- | --- | --- | --- |
| `SessionStart` | `idle` | 绿灯常亮 | `Codex 会话已开始` | 会话启动 / resume / clear / compact。它是瞬时事件，常被后续状态覆盖 |
| `UserPromptSubmit` | `thinking` | 黄灯呼吸 | `Codex 正在思考` | 用户 prompt 已提交，模型开始分析 |
| `SubagentStart` | `busy` | 红灯常亮 | `Codex 子任务正在运行` | 子 agent 开始运行，通常代表有实际工作在进行 |
| `PreToolUse` | `busy` | 红灯常亮 | `Codex 正在动手` | 即将执行工具 |
| `PermissionRequest` | `approval` | 红灯快闪 | `Codex 需要审批` | Codex 即将请求用户审批 |
| `PostToolUse` 成功 | `thinking` | 黄灯呼吸 | `Codex 正在思考` | 工具结束，模型继续判断下一步 |
| `PostToolUse` 失败 | `thinking` | 黄灯呼吸 | `Codex 工具执行失败，正在处理` | 工具失败后模型通常还会继续调整；失败详情看 `details.error` / `details.toolFailed` |
| `PreCompact` | `thinking` | 黄灯呼吸 | `Codex 正在压缩上下文` | 上下文压缩前 |
| `PostCompact` | `thinking` | 黄灯呼吸 | `Codex 已完成上下文压缩` | 上下文压缩后 |
| `SubagentStop` | `thinking` | 黄灯呼吸 | `Codex 子任务完成，正在整理` | 子 agent 结束，主流程可能还会继续 |
| `Stop` | `idle` | 绿灯常亮 | `Codex 空闲` | 当前 turn 结束 |

默认全局 Codex hooks 全量接入：

```text
SessionStart, SubagentStart, UserPromptSubmit, PreToolUse, PermissionRequest,
PostToolUse, PreCompact, PostCompact, SubagentStop, Stop
```

Codex `details` 会保留：

```text
hookEventName, knownCodexEvent, matcherValue, sessionId, turnId, cwd,
transcriptPath, model, permissionMode, prompt, source, sessionStartSource,
trigger, toolName, toolUseId, toolInputDescription, toolInput, toolResponse,
toolFailed, agentId, agentType, agentTranscriptPath, stopHookActive,
lastAssistantMessage, error, rawHook
```

Codex 注意事项：

```text
1. SessionStart / Stop 都是 idle。前者表示会话刚开始，后者表示当前 turn 完成。
2. /status 只保留最后一个状态；SessionStart 若已收到，也可能马上被 UserPromptSubmit、PreToolUse 或 Stop 覆盖。
3. 需要确认瞬时事件是否到达时，用 GET /api/devices/:id/events?limit=20&details=1。
4. PostToolUse 失败仍映射为 thinking，而不是单独 error 灯；因为 Codex 通常会继续修正。
5. SubagentStop 是全量观测用事件，可能短暂把灯盖回黄灯；只关心最终空闲时可以不接。
```

### Claude Code

官方生命周期事件当前全量接入。若只想让灯更安静，可以删掉高频事件，如 `MessageDisplay`、`FileChanged`、`CwdChanged`、`SubagentStop`。

| Claude Code event | state | 灯效 | message | 说明 |
| --- | --- | --- | --- | --- |
| `SessionStart` | `idle` | 绿灯常亮 | `Claude Code 会话已开始 (...)` | 会话开始 / resume / clear / compact |
| `Setup` | `thinking` | 黄灯呼吸 | `Claude Code 正在初始化 (...)` | 初始化 / maintenance |
| `UserPromptSubmit` | `thinking` | 黄灯呼吸 | `Claude Code 正在思考` | 用户 prompt 已提交 |
| `UserPromptExpansion` | `thinking` | 黄灯呼吸 | `Claude Code 正在展开指令...` | slash command / skill 展开 |
| `PreToolUse` | `busy` | 红灯常亮 | `Claude Code 正在执行工具: <tool>` | 即将执行工具 |
| `PermissionRequest` | `approval` | 红灯快闪 | `Claude Code 需要审批: <tool>` | 权限请求 |
| `PermissionDenied` | `thinking` | 黄灯呼吸 | `Claude Code 权限被拒，正在调整: <tool>` | 权限被拒后模型可能继续调整 |
| `PostToolUse` 成功 | `thinking` | 黄灯呼吸 | `Claude Code 工具执行完成，正在判断下一步` | 工具完成后继续响应 |
| `PostToolUseFailure` / 工具失败 | `thinking` | 黄灯呼吸 | `Claude Code 工具执行失败，正在处理` | 工具失败后继续由模型处理 |
| `PostToolBatch` | `thinking` | 黄灯呼吸 | `Claude Code 工具批次完成，正在继续处理` | 一批并行工具调用完成 |
| `Notification` + `permission_prompt` | `approval` | 红灯快闪 | `Claude Code 等待工具审批` | 需要审批工具使用 |
| `Notification` + `idle_prompt` | `approval` | 红灯快闪 | `Claude Code 已完成，等待你的下一步` | Claude 完成并等待输入，提醒你看一眼 |
| `Notification` + `elicitation_dialog` | `approval` | 红灯快闪 | `Claude Code MCP 请求用户输入` | MCP 请求用户输入 |
| `Notification` + 其他普通通知 | `thinking` | 黄灯呼吸 | `Claude Code 通知更新` | 普通状态通知 |
| `MessageDisplay` | `thinking` | 黄灯呼吸 | `Claude Code 正在显示回复` | 消息生成 / 展示中 |
| `SubagentStart` | `busy` | 红灯常亮 | `Claude Code 子任务正在运行...` | 子 agent 开始运行 |
| `SubagentStop` | `thinking` | 黄灯呼吸 | `Claude Code 子任务完成，正在整理...` | 子 agent 结束，可能盖过主流程 `Stop` |
| `TaskCreated` | `busy` | 红灯常亮 | `Claude Code 任务已创建: <task>` | 任务创建 |
| `TaskCompleted` | `thinking` | 黄灯呼吸 | `Claude Code 任务已完成，正在整理: <task>` | 任务完成后整理 |
| `Stop` | `idle` | 绿灯常亮 | `Claude Code 空闲` | 主 agent 本轮结束 |
| `StopFailure` | `idle` | 绿灯常亮 | `Claude Code 异常结束，已停止运行 (...)` | API / 系统错误导致回合结束 |
| `TeammateIdle` | `idle` | 绿灯常亮 | `Claude Code 队友空闲` | agent team 队友将空闲 |
| `InstructionsLoaded` | `thinking` | 黄灯呼吸 | `Claude Code 已加载指令 (...)` | 指令文件加载 |
| `ConfigChange` | `thinking` | 黄灯呼吸 | `Claude Code 配置已变更...` | 配置变更 |
| `CwdChanged` | `thinking` | 黄灯呼吸 | `Claude Code 工作目录已变更: <cwd>` | 工作目录变更 |
| `FileChanged` | `thinking` | 黄灯呼吸 | `Claude Code 监听文件已变更: <file>` | 监听文件变化 |
| `WorktreeCreate` | `busy` | 红灯常亮 | `Claude Code 正在创建 worktree: <path>` | 创建 worktree |
| `WorktreeRemove` | `busy` | 红灯常亮 | `Claude Code 正在移除 worktree: <path>` | 移除 worktree |
| `PreCompact` | `thinking` | 黄灯呼吸 | `Claude Code 正在压缩上下文 (...)` | 上下文压缩前 |
| `PostCompact` | `thinking` | 黄灯呼吸 | `Claude Code 已完成上下文压缩 (...)` | 上下文压缩后 |
| `Elicitation` | `approval` | 红灯快闪 | `Claude Code MCP 需要用户输入` | MCP 请求用户输入 |
| `ElicitationResult` | `thinking` | 黄灯呼吸 | `Claude Code MCP 用户输入已返回` | MCP 用户输入结果返回 |
| `SessionEnd` | `idle` | 绿灯常亮 | `Claude Code 会话已结束 (...)` | 会话结束 |

核心安静配置可以只接：

```text
SessionStart, UserPromptSubmit, PreToolUse, PostToolUse,
PostToolUseFailure, PermissionRequest, Notification,
Stop, StopFailure, SessionEnd
```

Claude `details` 会保留：

```text
sessionId, cwd, transcriptPath, prompt, toolName, toolInput,
notificationType, notificationText, sessionStartSource, matcherValue,
taskLabel, changedFile, worktreePath, errorType, error, reason,
knownClaudeCodeEvent, rawHook
```

注意：Claude Code 官方说明 `Stop` 在 Claude 完成响应时触发，但用户手动中断不会触发 `Stop`；API 错误结束会触发 `StopFailure`。

### Antigravity

官方 hook 事件：`PreInvocation`、`PostInvocation`、`PreToolUse`、`PostToolUse`、`Stop`。

| Antigravity event | state | 灯效 | 说明 |
| --- | --- | --- | --- |
| `PreInvocation` | `thinking` | 黄灯呼吸 | 模型调用前 |
| `PostInvocation` | `thinking` | 黄灯呼吸 | 工具调用阶段完成，执行循环可能继续 |
| `PreToolUse` 普通工具 | `busy` | 红灯常亮 | 即将执行工具，collector 返回 `{"decision":"allow"}` |
| `PreToolUse` 高危工具 | `approval` | 红灯快闪 | 需要审批，collector 返回 `ask` / `force_ask` |
| `PostToolUse` 成功 | `thinking` | 黄灯呼吸 | 工具完成，模型继续处理 |
| `PostToolUse` 失败 | `thinking` | 黄灯呼吸 | 工具失败后继续处理；失败看 `details.error` |
| `Stop` + `fullyIdle:true` | `idle` | 绿灯常亮 | 执行循环终止且后台任务完成 |
| `Stop` + `fullyIdle:false` | `busy` | 红灯常亮 | 仍有后台命令或异步任务 |
| `Stop` + 取消 / 中断 | `idle` | 绿灯常亮 | 执行已停止，原因保留在 `details.terminationReason` |

Antigravity 高危工具审批名单：

```text
run_command, ask_permission, write_to_file, replace_file_content,
multi_replace_file_content, search_web, read_url_content
```

Antigravity 的细状态要看 `event + details`，尤其是：

```text
toolName, toolArgs, stepIdx, conversationId, workspacePaths,
transcriptPath, artifactDirectoryPath, error, fullyIdle, terminationReason,
executionNum, invocationNum, initialNumSteps, decision, rawHook
```

## 运行配置

Claude Code 的 hooks 配置只需要 `hooks`，不需要在 `~/.claude/settings.json` 里写 `env`。本项目运行时配置分两类：

```text
server 推荐用命令参数；环境变量只是可选覆盖
collector 推荐改脚本顶部常量；环境变量只是可选覆盖
```

server 端可选环境变量：

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `AGENT_LIGHT_PORT` | `4318` | 监听端口 |
| `AGENT_LIGHT_HOST` | `127.0.0.1` | 监听地址；走反代保持默认，要让服务直接对外才设 `0.0.0.0` |
| `AGENT_LIGHT_COLLECTOR_TOKEN` | 随机生成 | collector 上报鉴权；不指定则每次启动生成新 token |
| `AGENT_LIGHT_DEVICE_TOKEN` | 随机生成 | 设备查询鉴权；不指定则每次启动生成新 token |
| `AGENT_LIGHT_IDLE_TTL_MS` | `1200000` | 超时未更新回落 idle |
| `AGENT_LIGHT_MAX_RECENT_EVENTS` | `100` | 每个 deviceId 独立保留的最近事件数，供 `/events` 调试 |

collector 端可选环境变量：

| 变量 | 默认 | 作用 |
| --- | --- | --- |
| `AGENT_LIGHT_DEVICE_ID` | `desk-light-01` | 这盏灯/这个用户的唯一 id；同用户所有工具填同一个，不同用户填不同的 |
| `AGENT_LIGHT_SERVER_URL` | 脚本内常量 | 上报地址 |
| `AGENT_LIGHT_COLLECTOR_TOKEN` | 脚本内常量 | 上报鉴权；填服务端启动时显示的 Collector token，或你固定指定的 token |
| `AGENT_LIGHT_POST_TIMEOUT_MS` | 脚本内默认 | POST 超时；Codex 默认 `3000`，Claude Code / Antigravity 默认 `800` |

调 TTL：

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
AGENT_LIGHT_IDLE_TTL_MS=300000 go run .
```

服务长期不开时降低等待：

```bash
AGENT_LIGHT_POST_TIMEOUT_MS=200
```

## 本地测试

启动 server：

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
go run .
```

手动发事件（直接 curl POST，无需脚本）：

```bash
curl -s http://127.0.0.1:4318/api/devices/desk-light-01/events \
  -H 'Authorization: Bearer <collector-token>' \
  -H 'Content-Type: application/json' \
  -d '{"source":"manual","state":"approval","event":"ManualTest","message":"测试"}'
```

`state` 取 `idle` / `thinking` / `busy` / `approval`。

查询状态：

```bash
curl -s -H 'Authorization: Bearer <device-token>' \
  http://127.0.0.1:4318/api/devices/desk-light-01/status
```

## 故障排查

| 现象 | 排查 |
| --- | --- |
| 灯一直不变 | server 起了吗？`curl http://127.0.0.1:4318/health` |
| server / 远程接收服务没开 | 不影响 Claude / Codex / Antigravity 正常使用；collector 上报失败会吞掉并正常退出，只是灯不更新 |
| 工具感觉变慢 | collector 默认最多等 `AGENT_LIGHT_POST_TIMEOUT_MS=800ms`；服务长期不开可调低到 `200` |
| 回合结束灯不转绿 | 检查是否接了 `SubagentStop`；Claude 默认不要接它；也可等 TTL 或手动 POST idle |
| collector 上报失败 | server 没起或超时？`curl http://127.0.0.1:4318/health` 确认在跑；失败已静默丢弃、不落盘 |
| Codex hooks 不触发 | `/hooks` 里 trust 了吗？配置里是否误加了 `async:true`？ |
| Claude Code hooks 没生效 | `jq . ~/.claude/settings.json` 检查 JSON；Claude 通常热加载 |
| Antigravity hooks 没生效 | 检查 `~/.gemini/config/hooks.json` 结构是否是命名组，不是顶层 `hooks` |
| 端口被占 | `lsof -nP -iTCP:4318` 找 PID，或换 `AGENT_LIGHT_PORT` |
| token 要改 | server 和 collector 的 `AGENT_LIGHT_COLLECTOR_TOKEN` 要一致；设备用 `AGENT_LIGHT_DEVICE_TOKEN` |

手动置空闲：

```bash
curl -s -X POST \
  -H 'Authorization: Bearer <collector-token>' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:4318/api/devices/desk-light-01/events \
  -d '{"source":"manual","state":"idle","event":"ManualReset","message":"手动复位"}'
```

## 项目规划

### 当前阶段

已完成：

```text
collector/codex/codex-hook.js
collector/claude-code/claude-hook.js
collector/antigravity/antigravity-hook.js
server/main.go
server/daemon.go
server/build.sh
firmware/  (ESP32-C3，见 firmware/README.md)
```

当前 server 是 Go 版：

```text
Go 标准库 net/http
内存存储（不落盘）
按 device-id 分区（默认示例 desk-light-01）
Bearer token 鉴权
每份 device：last-write-wins + TTL
支持 agent-light-server server start|stop|restart|status 后台运行
支持 darwin-arm64 / linux-amd64 编译
```

### 后续 server

可继续增强：

```text
SQLite 或 Redis
明确 schema 校验
设备管理
历史事件查询
```

### firmware（已实现）

ESP32-C3 SuperMini 固件，PlatformIO + Arduino，代码在 [`firmware/`](firmware/)。职责：

```text
连接 Wi-Fi
定时 GET /api/devices/:id/status（HTTP）
按 color/effect 控制红黄绿 LED（solid / breathing / fast_blink）
网络失败 30s → 全灭（掉线指示）
```

固件不理解三家 hooks，只理解 server 返回的统一状态。接线/烧录见 [`firmware/README.md`](firmware/README.md)。

### 可选能力

以后可加：

| 能力 | 用途 |
| --- | --- |
| `watchSources` | 设备只关注某些工具 |
| `watchWorkspaces` | 设备只关注某些项目 |
| `approvalOnlyMode` | 只有需要审批时提醒，其余保持绿灯 |
| `quietHours` | 特定时间不显示 thinking |
| Web dashboard | 查看当前状态和历史事件 |
| install script | 自动合并三家全局 hooks |

## 参考文档

- Codex hooks: https://developers.openai.com/codex/hooks
- Claude Code hooks: https://code.claude.com/docs/en/hooks
- Antigravity hooks: https://antigravity.google/docs/hooks
