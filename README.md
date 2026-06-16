# Agent Light

Agent Light 是一个把 AI 编程工具状态显示到实体灯上的小系统。

当前方案固定为：

```text
Codex / Claude Code / Antigravity hooks
        -> collector JS 脚本
        -> Agent Light Go Server
        -> MQTT
        -> WLED 设备 preset
        -> WS2812 / WS2812B 灯
```

设备端只使用 WLED 固件。服务端只负责状态、事件和 MQTT preset 推送，不在服务端维护具体灯效动画。

## 快速上手

更完整的逐步清单见 [QUICKSTART.md](QUICKSTART.md)。这里保留最短路径。

### 1. 准备

| 项 | 说明 |
| --- | --- |
| Server | 一台本机或服务器，运行 Go server |
| MQTT broker | mosquitto、EMQX、NAS MQTT、云 MQTT 都可以 |
| WLED 设备 | ESP32-C3 Pro Mini + 12 灯珠 WS2812B 环形灯珠 |
| Node.js | 18+，collector 脚本需要 |
| Go | 1.22+，运行 / 编译 server 需要 |

当前使用的硬件模型：

[Minecraft 矿石灯 WLED ESP32 D1 mini USB-C](https://makerworld.com.cn/zh/models/1710654-minecraft-kuang-shi-deng-wled-esp32-d1-mini-usb-c?appSharePlatform=wx#profileId-1882613)

### 2. 刷 WLED

1. 打开 [WLED Web Installer](https://install.wled.me)。
2. 连接 ESP32-C3 Pro Mini，选择串口并刷入 WLED。
3. 首次启动后连接 `WLED-AP`。
4. 打开 `http://4.3.2.1` 配 Wi-Fi。
5. 进入 WLED 页面，配置 LED 类型、GPIO、灯珠数量。

当前硬件建议：

| WLED 配置项 | 建议值 |
| --- | --- |
| LED 类型 | WS281x / WS2812 |
| LED 数量 | 12 |
| 色彩顺序 | 常见为 GRB，颜色不对再改 |
| GPIO | 按实际接线填写 |

设备端完整说明见 [firmware/README.md](firmware/README.md)。

### 3. 启动 Server

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
go run .
```

首次启动会自动生成：

```text
server/env.json
```

打开 `server/env.json`，填 MQTT：

```json
{
  "collectorToken": "自动生成，不要删",
  "deviceToken": "自动生成，不要删",
  "host": "127.0.0.1",
  "port": 4318,
  "idleTtlMs": 1200000,
  "maxRecentEvents": 100,
  "mqttBroker": "tcp://你的MQTT地址:1883",
  "mqttClientId": "agent-light-server",
  "mqttUser": "",
  "mqttPass": "",
  "mqttTopic": "wled/%s"
}
```

确认服务：

```bash
curl http://127.0.0.1:4318/health
```

后台运行、token、API、编译说明见 [server/README.md](server/README.md)。

### 4. 配 WLED MQTT

进入 WLED：

```text
Config -> Sync Interfaces -> MQTT
```

关键配置：

| 配置项 | 示例 |
| --- | --- |
| Enable MQTT | 勾选 |
| Broker | `你的MQTT地址`，不要带 `tcp://` |
| Port | `1883` |
| Device Topic | `wled/<deviceId>`，例如 `wled/desk-light-01` |

注意：

```text
WLED Device Topic 填 wled/<deviceId>
不要填 wled/<deviceId>/api
```

server 会自动发布到：

```text
wled/<deviceId>/api
```

### 5. 保存 WLED Preset

在每台 WLED 设备里保存 4 个 preset。服务端只调用 preset ID，具体动画在 WLED 里配置。

| Preset ID | 名称 | 状态 |
| --- | --- | --- |
| 1 | `Agent Idle` | 空闲 |
| 2 | `Agent Thinking` | 思考 |
| 3 | `Agent Busy` | 执行中 |
| 4 | `Agent Approval` | 等待审批 |

效果建议见 [docs/LIGHT_EFFECTS.md](docs/LIGHT_EFFECTS.md)。

### 6. 配 Collector 和 Hooks

先修改你要使用的 collector 脚本顶部配置：

```js
const DEVICE_ID = "desk-light-01";
const SERVER_URL = "http://127.0.0.1:4318";
const COLLECTOR_TOKEN = "填 server/env.json 里的 collectorToken";
```

然后把对应示例 hooks 合并到工具自己的全局配置里：

| 工具 | Collector | 全局配置 | 详细说明 |
| --- | --- | --- | --- |
| Codex | `collector/codex/codex-hook.js` | `~/.codex/hooks.json` | [collector/codex/README.md](collector/codex/README.md) |
| Claude Code | `collector/claude-code/claude-hook.js` | `~/.claude/settings.json` | [collector/claude-code/README.md](collector/claude-code/README.md) |
| Antigravity | `collector/antigravity/antigravity-hook.js` | `~/.gemini/config/hooks.json` | [collector/antigravity/README.md](collector/antigravity/README.md) |

原则：

```text
不要复制 collector 目录
不要整文件覆盖已有全局配置
只合并 Agent Light 相关 hooks
command 里写本项目 collector 脚本的绝对路径
```

Codex 配好后需要在 Codex 里运行 `/hooks` 并 trust。

### 7. 验证

手动发一个状态：

```bash
curl -s -X POST \
  -H 'Authorization: Bearer <collector-token>' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:4318/api/devices/desk-light-01/events \
  -d '{"source":"manual","state":"approval","event":"ManualTest","message":"测试审批"}'
```

查询状态：

```bash
curl -s -H 'Authorization: Bearer <device-token>' \
  http://127.0.0.1:4318/api/devices/desk-light-01/status
```

查询最近事件：

```bash
curl -s -H 'Authorization: Bearer <device-token>' \
  'http://127.0.0.1:4318/api/devices/desk-light-01/events?limit=20&details=1'
```

## 状态

Agent Light 统一为 4 个状态：

| state | 含义 | HTTP `color` | WLED preset |
| --- | --- | --- | --- |
| `idle` | 空闲 / 回合结束 | `green` | 1 |
| `thinking` | AI 正在思考、生成、整理 | `yellow` | 2 |
| `busy` | AI 正在执行工具或任务 | `red` | 3 |
| `approval` | 等待你审批或输入 | `red` | 4 |

完整状态模型见 [docs/STATUS_MODEL.md](docs/STATUS_MODEL.md)。三家工具事件映射见各 collector README。

## 文档导航

| 文档 | 内容 |
| --- | --- |
| [QUICKSTART.md](QUICKSTART.md) | 新手部署清单 |
| [server/README.md](server/README.md) | Go server、token、env.json、API、后台运行 |
| [firmware/README.md](firmware/README.md) | WLED 烧录、MQTT、preset、多灯隔离 |
| [collector/codex/README.md](collector/codex/README.md) | Codex hooks 配置与事件映射 |
| [collector/claude-code/README.md](collector/claude-code/README.md) | Claude Code hooks 配置与事件映射 |
| [collector/antigravity/README.md](collector/antigravity/README.md) | Antigravity hooks 配置与事件映射 |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 系统架构、职责边界、项目结构 |
| [docs/STATUS_MODEL.md](docs/STATUS_MODEL.md) | 统一状态、API 关系、deviceId 隔离 |
| [docs/LIGHT_EFFECTS.md](docs/LIGHT_EFFECTS.md) | WLED preset 和不同灯型光效建议 |
| [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) | 常见问题排查 |
| [BUILD.md](BUILD.md) | 本地构建 Go server |

## 构建

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
./build.sh all
```

输出：

```text
server/dist/agent-light-server-darwin-arm64
server/dist/agent-light-server-linux-amd64
```

更多见 [BUILD.md](BUILD.md)。

## 许可证

本项目使用 [MIT License](LICENSE)。
