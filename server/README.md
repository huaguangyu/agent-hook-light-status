# Agent Light Server

Go 版 Agent Light 服务端，接口兼容原来的 `dev-server.js`。

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

`Device token` 是设备/查询端读取权限，ESP32 或调试 curl 用它查询状态和最近事件。

```http
GET /api/devices/:deviceId/status
Authorization: Bearer <device-token>
```

如果不指定 token，服务端每次启动都会随机生成 `Collector token` 和 `Device token`。前台启动会直接打印；后台启动会在 `server start` 时打印，并写入运行状态文件。之后可以随时用 `server status` 查看当前运行中的 token：

```bash
./agent-light-server server status
```

collector 和设备查询必须使用当前运行中的 token。

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

注意：`restart` 会先停止旧进程再按当前命令参数启动新进程。若不指定 token，会重新随机生成 token。

### 其他启动参数

| 参数 | 环境变量 | 默认 | 说明 |
| --- | --- | --- | --- |
| `--host` | `AGENT_LIGHT_HOST` | `127.0.0.1` | 监听地址；要直接对外监听可用 `0.0.0.0` |
| `--port` | `AGENT_LIGHT_PORT` | `4318` | 监听端口 |
| `--collector-token` | `AGENT_LIGHT_COLLECTOR_TOKEN` | 随机生成 | collector 上报事件的写入 token |
| `--device-token` | `AGENT_LIGHT_DEVICE_TOKEN` | 随机生成 | ESP32 / 调试查询状态的读取 token |
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

它会进入 `server/` 目录，用 `go build` 编译 `agent-light-server`。当前服务端只用 Go 标准库，`CGO_ENABLED=0`，所以 mac arm64 和 linux x64 都可以直接交叉编译。

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
| `AGENT_LIGHT_COLLECTOR_TOKEN` | 随机生成 | collector 上报鉴权 |
| `AGENT_LIGHT_DEVICE_TOKEN` | 随机生成 | 设备查询鉴权 |
| `AGENT_LIGHT_IDLE_TTL_MS` | `1200000` | 超时未更新回落 idle |
| `AGENT_LIGHT_MAX_RECENT_EVENTS` | `100` | 每个 deviceId 独立保留最近事件数 |

## 多设备与事件日志

`deviceId` 就是一盏灯或一个用户的灯，例如 `desk-light-01`、`alice-light`。服务端按 `deviceId` 分桶保存状态和事件：

```text
/api/devices/alice-light/events -> 只影响 alice-light
/api/devices/bob-light/events   -> 只影响 bob-light
```

每个 `deviceId` 都独立保留最近 `AGENT_LIGHT_MAX_RECENT_EVENTS` 条事件，默认 100 条。不同 `deviceId` 的状态和事件不会混在一起。

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
设备查询：Authorization: Bearer <AGENT_LIGHT_DEVICE_TOKEN>
```
