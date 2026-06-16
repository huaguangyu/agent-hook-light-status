# Agent Light 设备端：使用 WLED

当前项目不再维护自定义 ESP32 / PlatformIO 固件。设备端统一使用 WLED 固件，Agent Light Server 通过 MQTT 给 WLED 的 `<topic>/api` 发布 preset 调用。

第一次部署建议先看根目录 [README.md](../README.md) 的“新手快速部署”。本文只展开 WLED 设备端怎么烧录、怎么配 MQTT、怎么保存 preset、怎么自测。

## 当前推荐硬件

当前项目默认按这套硬件记录和调试：

| 项 | 内容 |
| --- | --- |
| 外壳模型 | [Minecraft 矿石灯 WLED ESP32 D1 mini USB-C](https://makerworld.com.cn/zh/models/1710654-minecraft-kuang-shi-deng-wled-esp32-d1-mini-usb-c?appSharePlatform=wx#profileId-1882613) |
| 开发板 | ESP32-C3 Pro Mini |
| 灯板 | 12 灯珠 WS2812B 环形灯珠 |
| 固件 | WLED |
| 示例 `deviceId` | `desk-light-01` |
| 示例 WLED Device Topic | `wled/desk-light-01` |

WLED 里 LED 硬件参数按实际接线填写：

| WLED 配置项 | 建议值 |
| --- | --- |
| LED 类型 | WS281x / WS2812 |
| LED 数量 | 12 |
| 色彩顺序 | 常见为 GRB；若颜色不对再改 |
| GPIO | 按你的 ESP32-C3 Pro Mini 实际接线填写 |
| 布局 | 12 环形灯珠可直接用普通 strip 配置；效果在 preset 里调成旋转/流动即可 |

## 烧录 WLED

1. 打开 [WLED Web Installer](https://install.wled.me)。
2. 选择你的 ESP32 / ESP32-C3 串口并刷入 WLED。
3. 首次启动后连接 WLED 创建的热点，进入 WLED 配网页面。
4. 配置 Wi-Fi，让 WLED 接入与你的 MQTT broker 可互通的网络。
5. 配置 LED 硬件参数：LED 类型、GPIO、灯珠数量、矩阵或环形布局。

如果 WLED 后面连不上 Wi-Fi，通常会重新开 `WLED-AP` 热点。连接热点后打开：

```text
http://4.3.2.1
```

再进入 Wi-Fi 设置重新填写路由器 SSID 和密码。

## 配置 MQTT

进入 WLED 网页 UI：

```text
Config -> Sync Interfaces -> MQTT
```

填写：

| 配置项 | 示例 | 说明 |
| --- | --- | --- |
| MQTT Broker | `192.168.1.10` | 你的 MQTT broker 地址 |
| MQTT Port | `1883` | broker 端口 |
| MQTT User / Password | 按需填写 | broker 没鉴权可留空 |
| Device Topic | `wled/<deviceId>`，例如 `wled/desk-light-01` | 必须和 server 的 `deviceId` 对应 |

Agent Light Server 默认 topic 模板是：

```json
{
  "mqttTopic": "wled/%s"
}
```

如果 hooks 上报到：

```text
/api/devices/desk-light-01/events
```

server 会发布到：

```text
wled/desk-light-01/api
```

所以这盏 WLED 灯的 **Device Topic** 模板是：

```text
wled/<deviceId>
```

例如 `deviceId=desk-light-01` 时才填：

```text
wled/desk-light-01
```

注意：Device Topic **不要**填成：

```text
wled/<deviceId>/api
```

WLED 会自动监听 `<Device Topic>/api`。如果你在 Device Topic 里手动加了 `/api`，WLED 实际监听会变成 `wled/<deviceId>/api/api`，server 发到 `wled/<deviceId>/api` 时灯就不会响应。

WLED MQTT 配置保存后，如果页面提示需要重启，必须重启 WLED 才会生效。

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

如果 WLED 里已经保存了 Preset 1，灯应该切到 `Agent Idle`。

继续测试：

```text
T=1&PL=2
T=1&PL=3
T=1&PL=4
```

如果 MQTT 工具能看到消息但灯不变，优先检查：

| 检查项 | 正确值 |
| --- | --- |
| Enable MQTT | 已勾选 |
| Device Topic | `wled/<deviceId>`，例如 `wled/desk-light-01`，不带 `/api` |
| server `mqttTopic` | `wled/%s`，不带 `/api` |
| 发布 topic | `wled/<deviceId>/api`，例如 `wled/desk-light-01/api` |
| preset | 已保存 1-4 号 preset |
| WLED 配置变更后 | 已重启 |

## 多灯隔离

每个用户或每盏灯使用不同的 `deviceId`：

```text
alice-light -> WLED Device Topic: wled/alice-light
bob-light   -> WLED Device Topic: wled/bob-light
```

这样不同用户的灯不会互相串状态。

如果你希望多块 WLED 灯板显示同一个状态，就让它们使用同一个 Device Topic，例如都填 `wled/workspace`。

## 服务端 MQTT 配置

推荐在 `server/env.json` 填：

```json
{
  "collectorToken": "请替换为你的-collector-token",
  "deviceToken": "请替换为你的-device-token",
  "mqttBroker": "tcp://<broker-host>:1883",
  "mqttClientId": "agent-light-server",
  "mqttUser": "",
  "mqttPass": "",
  "mqttTopic": "wled/%s"
}
```

`mqttTopic` 含 `%s` 时，server 会把 `%s` 替换成 `deviceId`，并最终发布到 `<topic>/api`。

## 配置 WLED preset

server 只调用固定 preset，不在服务端维护具体灯效。你需要在每台 WLED 设备上保存：

| Preset ID | WLED preset 名称 | Agent state | 建议效果 |
| --- | --- | --- | --- |
| 1 | `Agent Idle` | `idle` | 低亮绿/青慢呼吸 |
| 2 | `Agent Thinking` | `thinking` | 黄/金呼吸或柔和流动 |
| 3 | `Agent Busy` | `busy` | 蓝/紫流动、扫描、流星 |
| 4 | `Agent Approval` | `approval` | 红/橙快闪或强提醒 |

WLED preset 名称只是给你在 UI 里识别用，MQTT 实际调用的是 preset ID。

保存 preset 的大致步骤：

```text
1. 在 WLED 首页调好颜色、效果、速度、亮度
2. 打开 Presets
3. 新建或覆盖指定 ID
4. Name 填 Agent Idle / Agent Thinking / Agent Busy / Agent Approval
5. 勾选保存当前状态
6. Save
```

WLED 实际收到的是 HTTP API 指令串，例如：

```text
T=1&PL=2
```

这表示打开灯并调用 WLED 设备端保存的 2 号 preset。

当前 4 个状态到 WLED preset 的映射在：

```text
server/mqtt.go
```

修改 `wledPresetByState` 后重启 server 即可生效。
