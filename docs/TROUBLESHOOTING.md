# 故障排查

## WLED 灯不变

优先检查：

| 检查项 | 正确值 |
| --- | --- |
| server 是否启动 | `curl http://127.0.0.1:4318/health` 返回 `ok` |
| server MQTT | `server/env.json` 里 `mqttBroker` 已填写 |
| WLED MQTT | Enable MQTT 已勾选 |
| WLED Broker / Port | 与 server 使用同一个 broker |
| WLED Device Topic | `wled/<deviceId>`，例如 `wled/desk-light-01`，不带 `/api` |
| server `mqttTopic` | `wled/%s`，不带 `/api` |
| WLED preset | 已保存 1-4 号 preset |
| WLED 配置变更后 | 已重启 |

最常见错误：

```text
WLED Device Topic 填成 wled/<deviceId>/api
```

正确应该是：

```text
WLED Device Topic: wled/<deviceId>
server 发布 topic:   wled/<deviceId>/api
```

## MQTT 自测

用 MQTT 工具订阅：

```text
wled/#
```

发布：

```text
Topic:   wled/<deviceId>/api
Payload: T=1&PL=1
QoS:     0
Retain:  false
```

例如 `deviceId=desk-light-01` 时，发布到 `wled/desk-light-01/api`。

灯应该切到 `Agent Idle`。再测试：

```text
T=1&PL=2
T=1&PL=3
T=1&PL=4
```

## collector 不上报

检查 collector 脚本顶部：

```js
const DEVICE_ID = "desk-light-01";
const SERVER_URL = "http://127.0.0.1:4318";
const COLLECTOR_TOKEN = "填 server/env.json 里的 collectorToken";
```

`COLLECTOR_TOKEN` 必须等于 `server/env.json` 里的 `collectorToken`。

手动发事件验证 server：

```bash
curl -s -X POST \
  -H 'Authorization: Bearer <collector-token>' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:4318/api/devices/desk-light-01/events \
  -d '{"source":"manual","state":"approval","event":"ManualTest","message":"测试审批"}'
```

## Codex hooks 不触发

检查：

```text
1. ~/.codex/hooks.json 是否合并了 hooks.example.json
2. command 是否是本项目 collector 脚本的绝对路径
3. Codex 里是否运行过 /hooks
4. /hooks 里是否 trust 了新增或 changed 的 hooks
5. 配置里不要误加 async:true
```

如果使用 cc-switch，信任记录在 `~/.codex/config.toml` 的 `[hooks.state]`，需要在 `/hooks` trust 后复制到 cc-switch 的 Codex 通用 `config.toml`。

## Claude Code hooks 不触发

检查：

```bash
jq . ~/.claude/settings.json
```

确认：

```text
1. JSON 合法
2. 顶层有 hooks
3. command 指向 collector/claude-code/claude-hook.js 的绝对路径
4. 不需要在 settings.json 里写 env
```

## Antigravity hooks 不触发

Antigravity 全局配置是命名组结构，不是顶层 `hooks`：

```json
{
  "agent-light": {
    "PreInvocation": []
  }
}
```

检查文件：

```text
~/.gemini/config/hooks.json
```

## 回合结束灯不转绿

可能原因：

| 原因 | 处理 |
| --- | --- |
| 某个高频事件晚于 `Stop` 到达 | 看 `/events?limit=20&details=1` |
| Claude 接了 `SubagentStop` | 如果只关心最终空闲，可以移除这个 hook |
| server 没收到 Stop | 检查对应工具 hooks 是否触发 |
| 长时间没事件 | 等 TTL 回落，或手动 POST idle |

手动置空闲：

```bash
curl -s -X POST \
  -H 'Authorization: Bearer <collector-token>' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:4318/api/devices/desk-light-01/events \
  -d '{"source":"manual","state":"idle","event":"ManualReset","message":"手动复位"}'
```

## 多用户设备串灯

检查：

```text
server mqttTopic = wled/%s
alice 的 WLED Device Topic = wled/alice-light
bob 的 WLED Device Topic   = wled/bob-light
```

collector 也要分别使用对应 `DEVICE_ID`。
