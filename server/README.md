# Agent Light Server

Go 版 Agent Light 服务端，接口兼容原来的 `dev-server.js`。

第一次部署建议先看根目录 [README.md](../README.md) 的“新手快速部署”。本文只展开服务端怎么启动、怎么配置 token / MQTT、怎么后台运行。

## 运行方式

前台运行：

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
go run .
```

后台运行：

```bash
./agent-light-server server start
./agent-light-server server status
./agent-light-server server stop
./agent-light-server server restart
```

### token 怎么指定

`Collector token` 是采集端写入权限，collector 用它上报事件。

```http
POST /api/devices/:deviceId/events
Authorization: Bearer <collector-token>
```

`Device token` 是 HTTP 查询读取权限，主要给调试 curl 和 Web 预览页使用。当前推荐的 WLED 方案由服务端主动推 MQTT，不需要 WLED 持有这个 token。

```http
GET /api/devices/:deviceId/status
Authorization: Bearer <device-token>
```

如果 `env.json` 不存在，服务端第一次启动会随机生成 `Collector token` 和 `Device token`，并写入 `env.json`。之后只要 `env.json` 还在，就会复用里面的 token。前台启动会直接打印 token；后台启动会在 `server start` 时打印，并写入运行状态文件。之后可以随时用 `server status` 查看当前运行中的 token：

```bash
./agent-light-server server status
```

collector 上报必须使用当前运行中的 collector token；HTTP 查询调试必须使用当前运行中的 device token。

推荐用命令参数固定 token：

```bash
./agent-light-server \
  --collector-token your-collector-token \
  --device-token your-device-token \
  server start
```

如果还要同时指定端口和监听地址：

```bash
./agent-light-server \
  --host 0.0.0.0 \
  --port 4318 \
  --collector-token your-collector-token \
  --device-token your-device-token \
  server start
```

也可以用环境变量固定 token：

```bash
AGENT_LIGHT_COLLECTOR_TOKEN=your-collector-token \
AGENT_LIGHT_DEVICE_TOKEN=your-device-token \
./agent-light-server server start
```

### env.json 配置文件（推荐，省去每次输入 token）

每次启动都带一长串 token 很麻烦。服务端会在**运行目录**下读取 `env.json`，把 token 和 MQTT 配置持久化在里面。

**首次启动时，如果 `env.json` 不存在，服务端会自动创建一份真实配置**：两个 token 会随机生成并写入文件，其他字段写入默认值。文件权限 `0600`。
仓库里只提交 `env.example.json`，真实的 `env.json` 已经加入 `.gitignore`，避免 token 和 MQTT 密码进 git。

```json
{
  "collectorToken": "请替换为你的-collector-token",
  "deviceToken": "请替换为你的-device-token",
  "host": "127.0.0.1",
  "port": 4318,
  "idleTtlMs": 1200000,
  "maxRecentEvents": 100,
  "mqttBroker": "tcp://<broker-host>:1883",
  "mqttClientId": "agent-light-server",
  "mqttUser": "",
  "mqttPass": "",
  "mqttTopic": "wled/%s"
}
```

配置优先级（高 -> 低）：

```text
命令行 flag  >  env.json  >  环境变量  >  默认值/随机生成
```

启动时如果 `env.json` 不存在，服务端会自动生成两个真实 token 并写入这个文件。用命令行参数启动时，例如 `--collector-token`、`--device-token`、`--port`、`--mqtt-broker`，最终生效值也会同步写回 `env.json`，后续可以直接启动，不用再带任何参数：

```bash
./agent-light-server server start
```

如果你手动把 `collectorToken` / `deviceToken` 留成占位符（含"请替换"字样），下次启动会把它当作未填写，重新生成真实 token 并写回 `env.json`。

### 常用后台命令

启动：

```bash
./agent-light-server \
  --collector-token your-collector-token \
  --device-token your-device-token \
  server start
```

查看状态和当前 token：

```bash
./agent-light-server server status
```

输出示例：

```text
服务运行中 (PID: 53717)
Address: http://127.0.0.1:4318
Collector token: your-collector-token
Device token: your-device-token
Idle TTL: 1200s
Max recent events per deviceId: 100
Started at: 2026-06-15 11:57:27
```

停止：

```bash
./agent-light-server server stop
```

重启：

```bash
./agent-light-server \
  --collector-token your-collector-token \
  --device-token your-device-token \
  server restart
```

注意：`restart` 会先停止旧进程再按当前命令参数启动新进程。若不指定 token，会继续使用 `env.json` 里的 token；如果 `env.json` 不存在，才会重新随机生成并写入。

### 其他启动参数

| 参数 | 环境变量 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--host` | `AGENT_LIGHT_HOST` | `127.0.0.1` | 监听地址；要直接对外监听可用 `0.0.0.0` |
| `--port` | `AGENT_LIGHT_PORT` | `4318` | 监听端口 |
| `--collector-token` | `AGENT_LIGHT_COLLECTOR_TOKEN` | `env.json` 或首次随机生成 | collector 上报事件的写入 token，最终值会写回 `env.json` |
| `--device-token` | `AGENT_LIGHT_DEVICE_TOKEN` | `env.json` 或首次随机生成 | HTTP 查询状态的读取 token，主要用于调试，最终值会写回 `env.json` |
| `--idle-ttl-ms` | `AGENT_LIGHT_IDLE_TTL_MS` | `1200000` | 超过多久没有新事件后回落 idle |
| `--max-recent-events` | `AGENT_LIGHT_MAX_RECENT_EVENTS` | `100` | 每个 deviceId 独立保留最近事件数量 |

后台模式会在程序目录创建：

```text
run/app.pid
run/app.json
run/app.log
```

`run/app.json` 保存当前后台进程的 token、地址和启动时间，权限为 `0600`，停止服务后会清理。

## 编译

Go 构建脚本是：

```text
/Users/apple/user/VscodeProject/agent_light/server/build.sh
```

它会进入 `server/` 目录，用 `go build` 编译 `agent-light-server`。服务端依赖 `github.com/eclipse/paho.mqtt.golang`（WLED MQTT 推送用），`CGO_ENABLED=0`，所以 mac arm64 和 linux x64 都可以直接交叉编译。

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
chmod +x build.sh
./build.sh all
```

产物：

```text
dist/agent-light-server-darwin-arm64
dist/agent-light-server-linux-amd64
```

也可以单独编译：

```bash
./build.sh darwin-arm64
./build.sh linux-amd64
./build.sh linux-x64
```

命令含义：

| 命令 | 说明 | 输出 |
| --- | --- | --- |
| `./build.sh all` | 同时构建 mac arm64 和 linux x64 | `dist/agent-light-server-darwin-arm64`、`dist/agent-light-server-linux-amd64` |
| `./build.sh darwin-arm64` | 只构建 Apple Silicon macOS | `dist/agent-light-server-darwin-arm64` |
| `./build.sh linux-amd64` | 只构建 Linux x64 | `dist/agent-light-server-linux-amd64` |
| `./build.sh linux-x64` | `linux-amd64` 的别名 | `dist/agent-light-server-linux-amd64` |

## 环境变量

服务端推荐用命令参数；环境变量只是可选覆盖。

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `AGENT_LIGHT_PORT` | `4318` | 监听端口 |
| `AGENT_LIGHT_HOST` | `127.0.0.1` | 监听地址 |
| `AGENT_LIGHT_COLLECTOR_TOKEN` | `env.json` 或首次随机生成 | collector 上报鉴权，最终值会写回 `env.json` |
| `AGENT_LIGHT_DEVICE_TOKEN` | `env.json` 或首次随机生成 | HTTP 查询鉴权，主要用于 curl 调试和 Web 预览页，最终值会写回 `env.json` |
| `AGENT_LIGHT_IDLE_TTL_MS` | `1200000` | 超时未更新回落 idle |
| `AGENT_LIGHT_MAX_RECENT_EVENTS` | `100` | 每个 deviceId 独立保留最近事件数 |
| `AGENT_LIGHT_MQTT_BROKER` | 空（不启用） | MQTT broker 地址，例如 `tcp://192.168.1.10:1883` |
| `AGENT_LIGHT_MQTT_TOPIC` | `wled/%s` | WLED topic 模板，`%s` 会替换为 `deviceId`，实际发送到 `<topic>/api` |
| `AGENT_LIGHT_MQTT_USER` | 空 | MQTT 用户名（可选） |
| `AGENT_LIGHT_MQTT_PASS` | 空 | MQTT 密码（可选） |

## 接 WLED（MQTT preset 推送）

当前推荐设备方案是 WLED + MQTT：服务端把 agent 状态变化通过 MQTT 直接推给刷了 WLED 固件的 ESP32，并调用 WLED 设备端保存的 preset。**collector 和上报链路完全不用改**，仍然是 4 个状态：`idle / thinking / busy / approval`。

职责边界：

| 模块 | 职责 |
| --- | --- |
| collector | 只采集 hooks 事件、映射状态、POST 到 server |
| server | 保存状态和事件、按 `deviceId` 隔离、状态变化时发布 MQTT preset 调用 |
| WLED 设备 | 配 Wi-Fi、MQTT、LED 硬件参数，并保存 1-4 号 preset |

### 状态 -> WLED preset 映射

服务端只发 preset 调用，不维护具体 FX、颜色、速度、亮度。

| 状态 | MQTT payload | WLED preset ID | WLED preset 名称 |
| --- | --- | --- | --- |
| `idle` | `T=1&PL=1` | 1 | `Agent Idle` |
| `thinking` | `T=1&PL=2` | 2 | `Agent Thinking` |
| `busy` | `T=1&PL=3` | 3 | `Agent Busy` |
| `approval` | `T=1&PL=4` | 4 | `Agent Approval` |

服务端会在状态**真正变化时**才发一次 MQTT（同一个 `deviceId` 的相同状态不重复发），消息发到对应 WLED 的 `<topic>/api`。

### 多用户 / 多设备 MQTT 隔离

推荐把服务端 `mqttTopic` 配成带 `%s` 的模板：

```json
{
  "mqttTopic": "wled/%s"
}
```

这样 HTTP 上报里的 `deviceId` 会自动映射到不同的 WLED topic：

```text
POST /api/devices/alice-light/events  ->  MQTT: wled/alice-light/api
POST /api/devices/bob-light/events    ->  MQTT: wled/bob-light/api
```

对应地，每个 WLED 设备进入 WLED 网页 UI → Config → Sync Interfaces → MQTT，把 **Device Topic** 分别填成自己的 topic：

```text
alice 的灯：wled/alice-light
bob 的灯：  wled/bob-light
```

这样每个用户/灯只订阅自己的状态，不会互相打架。服务端内部的 MQTT 去重也是按 `deviceId` 分开的，`alice-light` 的 `busy` 不会影响 `bob-light` 的 `idle`。

如果你只有一盏灯，也可以继续用固定 topic，例如 `wled/desk-ring`。固定 topic 不含 `%s` 时，所有 `deviceId` 都会发到同一个 `wled/desk-ring/api`，这是单设备兼容模式。

### WLED MQTT 注意事项

WLED 的 **Device Topic 不要带 `/api`**。

正确：

```text
WLED Device Topic: wled/desk-light-01
server 发布 topic:   wled/desk-light-01/api
```

错误：

```text
WLED Device Topic: wled/desk-light-01/api
```

WLED 会自动监听 `<Device Topic>/api`。如果 Device Topic 已经带 `/api`，WLED 实际监听会变成 `wled/desk-light-01/api/api`，server 发到 `wled/desk-light-01/api` 就不会触发灯效。

服务端配置保持：

```json
{
  "mqttTopic": "wled/%s"
}
```

不要改成 `wled/%s/api`，因为服务端代码会自动追加 `/api`。

WLED MQTT 配置保存后如果页面提示需要重启，必须重启 WLED。

MQTT 工具自测：

```text
Subscribe: wled/#
Publish topic: wled/desk-light-01/api
Payload: T=1&PL=1
QoS: 0
Retain: false
```

再依次测试 `T=1&PL=2`、`T=1&PL=3`、`T=1&PL=4`。如果消息能看到但灯不变，优先检查 Device Topic 是否误填了 `/api`，以及 WLED 是否已经保存对应 preset。

### 对接步骤

1. **ESP32 刷 WLED 固件**：访问 `https://install.wled.me` 直接刷，或下载 release 的 bin。ESP32-C3 支持。
2. **WLED 配置**：进 WLED 网页 UI → WiFi Settings 连 WiFi；配置 LED 类型、GPIO、灯珠数量和布局；→ Sync Settings → MQTT Connectivity 填 broker 地址和 Device Topic。多设备推荐按 `deviceId` 填，例如 `wled/desk-light-01`。
3. **准备 MQTT broker**：本机装 mosquitto，或用 EMQX / 公共 broker 均可。
4. **保存 WLED preset**：在每台 WLED 设备里保存 1-4 号 preset：`1=Agent Idle`、`2=Agent Thinking`、`3=Agent Busy`、`4=Agent Approval`。
5. **服务端填 env.json**：

   ```json
   {
     "mqttBroker": "tcp://192.168.1.10:1883",
     "mqttTopic": "wled/%s"
   }
   ```

6. **重启服务端**。`server start` 输出里会看到 `MQTT broker: tcp://... (topic 模板: wled/%s -> <deviceId>/api)`，agent 一活动，对应 `deviceId` 的 WLED 灯就会调用对应 preset。

不配置 `mqttBroker`（留空）时，MQTT 推送自动禁用；HTTP 状态接口仍可用于调试。

## 多设备与事件日志

`deviceId` 就是一盏灯或一个用户的灯，例如 `desk-light-01`、`alice-light`。服务端按 `deviceId` 分桶保存状态和事件：

```text
/api/devices/alice-light/events -> 只影响 alice-light
/api/devices/bob-light/events   -> 只影响 bob-light
```

每个 `deviceId` 都独立保留最近 `AGENT_LIGHT_MAX_RECENT_EVENTS` 条事件，默认 100 条。不同 `deviceId` 的状态和事件不会混在一起。

如果同一个状态通道要同时驱动多个 WLED 设备，推荐让这些 WLED 设备配置相同的 Device Topic，例如都订阅 `wled/workspace`。

## API

```text
POST /api/devices/:deviceId/events
GET  /api/devices/:deviceId/status
GET  /api/devices/:deviceId/events?limit=20&details=1
GET  /health
```

鉴权保持不变：

```text
collector 上报：Authorization: Bearer <AGENT_LIGHT_COLLECTOR_TOKEN>
HTTP 查询调试：Authorization: Bearer <AGENT_LIGHT_DEVICE_TOKEN>
```

`GET /status` 主要用于调试和 Web 预览页；当前 WLED 主方案通过 MQTT 接收 `<topic>/api` preset 调用，不需要调用这个接口。

响应只保留通用状态字段和一个简单 `color`，不再由服务端生成 `light`、`effect`、`display`：

```json
{
  "state": "approval",
  "color": "red",
  "message": "Codex 需要审批",
  "source": "codex",
  "event": "PermissionRequest",
  "updatedAt": "2026-06-13 18:40:46"
}
```
