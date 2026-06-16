# Agent Light 新手部署清单

这份清单只保留最短路径：让 WLED 灯能跟随 Codex / Claude Code / Antigravity 状态变化。

## 1. 准备

你需要：

| 项 | 说明 |
| --- | --- |
| 一台运行 server 的电脑或服务器 | 本机调试可以直接用 Mac |
| 一个 MQTT broker | 例如 mosquitto、EMQX、NAS MQTT、云 MQTT |
| 一块 ESP32-C3 Pro Mini | 刷 WLED |
| 12 灯珠 WS2812B 环形灯珠 | 当前默认硬件 |
| Node.js 18+ | collector 脚本需要 |
| Go 1.22+ | 编译 / 运行 server 需要 |

当前推荐硬件外壳模型：

```text
https://makerworld.com.cn/zh/models/1710654-minecraft-kuang-shi-deng-wled-esp32-d1-mini-usb-c?appSharePlatform=wx#profileId-1882613
```

## 2. 刷 WLED 固件

设备端只使用 WLED 固件，不再使用本项目自写 PlatformIO 固件。

1. 打开 [WLED Web Installer](https://install.wled.me)。
2. 用 USB 连接 ESP32-C3 Pro Mini。
3. 在浏览器里选择串口，刷入 WLED。
4. 首次启动后连接 WLED 创建的热点。
5. 打开 `http://4.3.2.1`，配置 Wi-Fi。
6. 进入 WLED 页面后配置 LED 硬件参数。

当前硬件建议：

| WLED 配置项 | 建议值 |
| --- | --- |
| LED 类型 | WS281x / WS2812 |
| LED 数量 | 12 |
| 色彩顺序 | 常见为 GRB；颜色不对再改 |
| GPIO | 按你的 ESP32-C3 Pro Mini 实际接线填写 |

如果 WLED 后续连不上 Wi-Fi，它通常会重新开启 `WLED-AP` 热点。重新连接热点后仍然访问 `http://4.3.2.1` 修改网络配置。

## 3. 启动 server

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
go run .
```

第一次启动会生成：

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

重启 server。确认正常：

```bash
curl http://127.0.0.1:4318/health
```

## 4. 配 WLED MQTT

进入 WLED 页面：

```text
Config -> Sync Interfaces -> MQTT
```

填写：

| 配置项 | 示例 |
| --- | --- |
| Enable MQTT | 勾选 |
| Broker | `你的MQTT地址`，不要带 `tcp://` |
| Port | `1883` |
| Username / Password | 按 broker 实际填写 |
| Device Topic | `wled/desk-light-01` |
| Group Topic | `wled/all` |

保存后，如果 WLED 提示需要重启，必须重启。

当前硬件默认值：

| 项 | 值 |
| --- | --- |
| 外壳模型 | [Minecraft 矿石灯 WLED ESP32 D1 mini USB-C](https://makerworld.com.cn/zh/models/1710654-minecraft-kuang-shi-deng-wled-esp32-d1-mini-usb-c?appSharePlatform=wx#profileId-1882613) |
| 开发板 | ESP32-C3 Pro Mini |
| 灯板 | 12 灯珠 WS2812B 环形灯珠 |
| WLED LED 类型 | WS281x / WS2812 |
| LED 数量 | 12 |
| 灯型 | 12 环形 |

注意：

```text
Device Topic 填 wled/desk-light-01
不要填 wled/desk-light-01/api
```

## 5. 保存 WLED Preset

在每台 WLED 设备上保存 4 个 preset：

| Preset ID | 名称 | 用途 |
| --- | --- | --- |
| 1 | `Agent Idle` | 空闲 |
| 2 | `Agent Thinking` | 思考 |
| 3 | `Agent Busy` | 执行中 |
| 4 | `Agent Approval` | 等待审批 |

保存方法：

```text
1. 在 WLED 首页调好颜色、效果、速度、亮度
2. 打开 Presets
3. 新建或覆盖指定 ID
4. Name 填 Agent Idle / Agent Thinking / Agent Busy / Agent Approval
5. 勾选保存当前状态
6. Save
```

## 6. MQTT 自测

用 MQTT 工具订阅：

```text
wled/#
```

发布：

```text
Topic: wled/desk-light-01/api
Payload: T=1&PL=1
```

灯应该切到 Preset 1。再测试：

```text
T=1&PL=2
T=1&PL=3
T=1&PL=4
```

## 7. 改 collector 配置

选择你要接入的工具，打开对应脚本：

| 工具 | 脚本 |
| --- | --- |
| Codex | `collector/codex/codex-hook.js` |
| Claude Code | `collector/claude-code/claude-hook.js` |
| Antigravity | `collector/antigravity/antigravity-hook.js` |

把顶部改成：

```js
const DEVICE_ID = "desk-light-01";
const SERVER_URL = "http://127.0.0.1:4318";
const COLLECTOR_TOKEN = "填 server/env.json 里的 collectorToken";
```

远程 server 时，`SERVER_URL` 改成远程地址。

## 8. 安装 hooks

按你使用的工具看对应文档：

| 工具 | 文档 |
| --- | --- |
| Codex | `collector/codex/README.md` |
| Claude Code | `collector/claude-code/README.md` |
| Antigravity | `collector/antigravity/README.md` |

核心原则：

```text
不要复制 collector 目录
只把 hooks 配置合并到工具自己的全局配置
command 里写本项目 collector 脚本的绝对路径
```

三家工具的全局配置文件分别是：

| 工具 | 全局配置文件 | 示例文件 |
| --- | --- | --- |
| Codex | `~/.codex/hooks.json` | `collector/codex/hooks.example.json` |
| Claude Code | `~/.claude/settings.json` | `collector/claude-code/settings.example.json` |
| Antigravity | `~/.gemini/config/hooks.json` | `collector/antigravity/hooks.example.json` |

注意：这些配置文件里只登记 hook 命令，真正的 collector 脚本仍然留在本项目目录。配置完成后，Codex 需要在工具里运行 `/hooks` 并信任新增 hook。

## 9. 验证完整链路

手动发一个审批状态：

```bash
curl -s -X POST \
  -H 'Authorization: Bearer <collector-token>' \
  -H 'Content-Type: application/json' \
  http://127.0.0.1:4318/api/devices/desk-light-01/events \
  -d '{"source":"manual","state":"approval","event":"ManualTest","message":"测试审批"}'
```

查状态：

```bash
curl -s -H 'Authorization: Bearer <device-token>' \
  http://127.0.0.1:4318/api/devices/desk-light-01/status
```

查最近事件：

```bash
curl -s -H 'Authorization: Bearer <device-token>' \
  'http://127.0.0.1:4318/api/devices/desk-light-01/events?limit=20&details=1'
```

## 10. 常见问题

| 现象 | 优先检查 |
| --- | --- |
| MQTT 工具能看到消息，WLED 不变 | WLED Device Topic 是否误填了 `/api`；WLED 是否重启；Preset 1-4 是否存在 |
| `/status` 有变化，灯不变 | `server/env.json` 里的 `mqttBroker` 是否填写；server 日志是否有 `[mqtt]` |
| collector 不上报 | collector 脚本里的 `COLLECTOR_TOKEN` 是否等于 `server/env.json` 的 `collectorToken` |
| Codex hooks 不触发 | Codex 里运行 `/hooks` 并 trust |
| Claude hooks 不触发 | 检查 `~/.claude/settings.json` 是否是合法 JSON |
| Antigravity hooks 不触发 | 检查 `~/.gemini/config/hooks.json` 是否是命名组结构 |
