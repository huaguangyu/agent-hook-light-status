# 架构说明

Agent Light 当前只保留一条主线方案：

```text
AI 工具 hooks -> collector JS -> Go server -> MQTT -> WLED preset -> WS2812 灯
```

设备端统一使用 WLED 固件。项目不再维护自定义 ESP32 / PlatformIO 固件。

## 数据流

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
        | MQTT publish: wled/<deviceId>/api  (T=1&PL=<preset>)
        v
WLED 设备
        |
        v
WS2812 / WS2812B 灯板
```

## 职责边界

| 模块 | 做什么 | 不做什么 |
| --- | --- | --- |
| collector | 读取三家工具的 hooks stdin，识别事件，映射成 `idle / thinking / busy / approval`，POST 给 server | 不直连灯、不连 MQTT、不保存状态 |
| server | 鉴权、按 `deviceId` 保存当前状态和最近事件、TTL 回落 idle、把状态转换成 WLED preset 调用并推到 MQTT | 不生成 `light/effect/display` 字段、不维护具体灯珠动画 |
| MQTT broker | 转发 server 发布的 WLED API 消息 | 不理解业务状态 |
| WLED 设备 | 订阅自己的 Device Topic，执行 preset，真正控制 WS2812 灯板 | 不主动查询 `/status`，不解析 Agent Light 自定义 JSON |

## deviceId 隔离

每个 `deviceId` 表示一个状态通道，也通常对应一盏灯或一个用户的灯：

```text
/api/devices/alice-light/events -> MQTT wled/alice-light/api
/api/devices/bob-light/events   -> MQTT wled/bob-light/api
```

每台 WLED 设备的 Device Topic 分别填：

```text
alice-light -> wled/alice-light
bob-light   -> wled/bob-light
```

这样不同用户/灯不会互相串状态。

如果多台 WLED 设备要显示同一个状态，就让它们使用同一个 Device Topic，例如都填 `wled/workspace`。

## 项目结构

```text
agent_light/
  collector/
    codex/
    claude-code/
    antigravity/
  server/
    main.go
    daemon.go
    mqtt.go
    envconfig.go
    build.sh
    README.md
  firmware/
    README.md
  docs/
    ARCHITECTURE.md
    STATUS_MODEL.md
    LIGHT_EFFECTS.md
    TROUBLESHOOTING.md
```

## 运行配置

真实运行配置在 `server/env.json`，这个文件不提交到 git。

首次启动 server 时，如果 `env.json` 不存在，会自动生成真实 `collectorToken` / `deviceToken` 并写入文件。命令行传入的 token、端口、MQTT 参数也会同步写回 `env.json`。

更多见 [server/README.md](../server/README.md)。
