# Agent Light 固件（ESP32-C3 SuperMini）

连 Wi-Fi → 定时 HTTP 轮询 server → 按 `color`/`effect` 点亮 GPIO0/1/2 的绿/黄/红灯。

PlatformIO + Arduino 框架。

## 接线

| LED | ESP32-C3 GPIO |
| --- | --- |
| 绿灯正极 | GPIO 0 |
| 黄灯正极 | GPIO 1 |
| 红灯正极 | GPIO 2 |
| 三灯负极 | GND |

- LED 正极串一个限流电阻（220Ω~1kΩ 都行，看你想多亮）。负极接 GND。
- active-high：GPIO 拉高 = 亮。固件用 PWM 调亮度（呼吸/快闪）。
- GPIO2 在 ESP32-C3 上是 strapping 脚，启动早期可能出现微亮；固件会在 `setup()` 最开始关灯，但 ROM/bootloader 阶段无法由软件控制。若想完全避免启动微亮，建议把红灯从 GPIO2 换到普通 GPIO，或给 LED 控制端加明确下拉。

## 改配置

打开 [`src/main.cpp`](src/main.cpp) 顶部的「改这里」块，填你的：

```cpp
const char* WIFI_SSID     = "your-wifi-ssid";
const char* WIFI_PASSWORD = "your-wifi-password";
const char* SERVER_HOST   = "your-server.example.com";
const char* DEVICE_ID     = "desk-light-01";   // 跟该用户 collector 里的 DEVICE_ID 一致
const char* DEVICE_TOKEN  = "<device-token>";
```

`DEVICE_ID` 决定这盏灯查谁的状态——同一个用户的所有 collector 和他的灯填同一个值。

## 烧录 & 看日志

```bash
cd firmware
pio run -t upload          # 编译并烧录（USB 连着 SuperMini）
pio device monitor         # 看串口日志（和烧录用同一根 USB 线）
```

或用 VSCode PlatformIO 插件的 ✅(build) → →(upload) → 🔌(monitor) 按钮。

烧录后开机会先自检：绿 → 黄 → 红各亮 400ms，方便确认三灯接线都通。然后连 Wi-Fi、开始轮询。

## Wi-Fi 连接排查

固件参考了 e-ink 项目的连接策略：每次连接最多尝试 3 次；每次失败后会 `disconnect(true)` 并短暂关闭 Wi-Fi radio，再重新进入 `WIFI_STA` 连接，避免 ESP32-C3 卡在旧连接状态里。

软件层面已做的优化：

- 关闭 Wi-Fi 省电：`WiFi.setSleep(false)`，减少低功耗切换导致的延迟和抖动。
- 提高发射功率：`WiFi.setTxPower(WIFI_POWER_19_5dBm)`。
- 记住上次成功连接的 AP `BSSID/channel`，下次优先定向连接，减少全信道扫描耗时。
- 定向连接失败后自动退回普通连接，不会因为路由器换信道而一直卡住。

串口日志里重点看这些行：

```text
[wifi] cached AP channel=... bssid=...
[wifi] connecting, attempt=1/3 ssid=...
[wifi] using cached AP channel=... bssid=...
[wifi] status=...
[wifi] connected, IP=... RSSI=... channel=... BSSID=...
```

如果 3 次都失败，固件会扫描同名 Wi-Fi：

- `SSID "... " not visible`：板子没看到这个 Wi-Fi，优先检查 SSID、路由器距离、2.4G/5G。ESP32-C3 只能连 2.4G。
- `found ssid=... rssi=...` 但仍连不上：优先检查密码、路由器加密方式、MAC 过滤。
- `RSSI` 低于 `-75` 左右时，连接会很不稳定，建议靠近路由器测试。

注意：`RSSI` 是板子收到路由器信号的强度，软件不能真正放大接收天线；上面的优化主要能提升连接速度和稳定性。若 RSSI 长期很低，最终还是要靠近路由器、减少遮挡、换 2.4G 信道，或换带外置天线的开发板。

## 状态 → 灯效

server 返回的 `color` 决定亮哪盏，`effect` 决定怎么亮：

| server 状态 | color | effect | 灯 |
| --- | --- | --- | --- |
| idle（空闲/超时） | green | solid | 绿灯常亮 |
| thinking（思考） | yellow | breathing | 黄灯呼吸 |
| busy（忙） | red | solid | 红灯常亮 |
| approval（待审批） | red | fast_blink | 红灯快闪 |

掉线（连续 30s 没拿到新状态）→ 三灯全灭，表示失联。

## 备注

- 走的是**明文 HTTP**，`DEVICE_TOKEN` 在 Authorization 头里明文传。当前 token 是只读查询用；要更稳就给 server 上 HTTPS（ESP32 走 `WiFiClientSecure` + 根证书）。
- 轮询间隔 `POLL_MS` 默认 3s，可按需调大省流量。
