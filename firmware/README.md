# Agent Light 固件（WS2812 / WS2812B）

ESP32 通过 Wi-Fi 定时查询 Agent Light Server，然后用 WS2812 灯板显示 `light.intent`。

支持两块开发板：

| 开发板 | PlatformIO env | 默认 WS2812 DATA GPIO |
| --- | --- | --- |
| ESP32-C3 | `esp32-c3` | GPIO 8 |
| ESP32-WROOM-32E | `esp32-wroom32e` | GPIO 4 |

默认配网按键：

| 开发板 | 默认配网按键 GPIO | 说明 |
| --- | --- | --- |
| ESP32-C3 | GPIO 9 | 很多 C3 板子的 BOOT 键；如果按住开机会进入下载模式，就外接按钮并改 `DEFAULT_CFG_BUTTON_PIN` |
| ESP32-WROOM-32E | GPIO 0 | 很多 WROOM32E 板子的 BOOT 键；如果影响启动，就外接按钮到普通 GPIO |

按钮接法：GPIO 一端，GND 一端。固件用 `INPUT_PULLUP`，按下为低电平。

支持多种灯板，直接在 [`src/main.cpp`](src/main.cpp) 顶部改 `LIGHT_PRESET`：

```cpp
#define LIGHT_PRESET PRESET_MATRIX_8X8
```

可选值：

| preset | layout | 灯板 |
| --- | --- | --- |
| `PRESET_MATRIX_8X8` | `matrix8x8` | 8x8 WS2812 方阵，推荐主显示 |
| `PRESET_MATRIX_4X4` | `matrix4x4` | 4x4 方阵 |
| `PRESET_MATRIX_2X2` | `matrix2x2` | 2x2 方阵 |
| `PRESET_RING_12` | `ring12` | 12 灯环 |
| `PRESET_BAR_6` | `bar6` | 6 位条形 |
| `PRESET_PIXEL_1` | `pixel1` | 单颗 WS2812 |

## 接线

WS2812 只需要一根数据线：

```text
ESP32 GPIO -> 330Ω~470Ω 电阻 -> WS2812 DIN
ESP32 GND  -> WS2812 GND
5V         -> WS2812 5V
```

注意：

- ESP32 和灯板必须共地。
- WS2812 / WS2812B 通常是 5V 供电。
- 少量灯珠、短线时 ESP32 3.3V 数据线通常可用；正式做建议加 74AHCT125 / 74HCT245 电平转换。
- 8x8 共有 64 颗灯，USB 供电时不要满白满亮。固件默认限制最大亮度 `MAX_BRIGHTNESS = 140`。
- 灯板 5V/GND 入口建议并一个 470µF~1000µF 电容。

## 切换开发板

开发板切换在 [`platformio.ini`](platformio.ini) 里通过 env 完成。

ESP32-C3：

```bash
cd firmware
pio run -e esp32-c3 -t upload
pio device monitor -e esp32-c3
```

ESP32-WROOM-32E：

```bash
cd firmware
pio run -e esp32-wroom32e -t upload
pio device monitor -e esp32-wroom32e
```

默认 env 是 `esp32-c3`，直接执行 `pio run -t upload` 会烧录 C3。

## 切换灯板

打开 [`src/main.cpp`](src/main.cpp)，改这一行：

```cpp
#define LIGHT_PRESET PRESET_MATRIX_8X8
```

例如切到 12 灯环：

```cpp
#define LIGHT_PRESET PRESET_RING_12
```

例如切到 4x4：

```cpp
#define LIGHT_PRESET PRESET_MATRIX_4X4
```

如果你的 8x8 / 4x4 显示方向或蛇形顺序不对，改这两行：

```cpp
#define MATRIX_SERPENTINE 1
#define MATRIX_ORIGIN_TOP_LEFT 1
```

常见情况：

| 现象 | 修改 |
| --- | --- |
| 奇数行方向反了 | 在 `0` / `1` 之间切换 `MATRIX_SERPENTINE` |
| 上下颠倒 | 在 `0` / `1` 之间切换 `MATRIX_ORIGIN_TOP_LEFT` |
| 红绿颜色反了 | 把 `COLOR_ORDER GRB` 改成 `COLOR_ORDER RGB` |

## 配网与服务配置

Wi-Fi、服务端地址、`DEVICE_ID`、`DEVICE_TOKEN` 都可以在配网页里改，保存后写入 ESP32 的 NVS flash，断电不丢。

首次启动时，如果没有可用配置，设备会自动开启 AP 配网模式：

```text
热点名称：AgentLight-xxxxx
配网页：  http://192.168.4.1
```

进入配网模式的方式：

| 方式 | 行为 |
| --- | --- |
| 首次启动无配置 | 自动进入 AP 配网 |
| 开机时按住配网键 | 强制进入 AP 配网 |
| 运行时长按配网键约 8 秒 | 进入 AP 配网 |
| 配网页 5 分钟无人操作 | 关闭热点，进入退避休眠 |

配网页可修改：

| 字段 | 说明 |
| --- | --- |
| Wi-Fi SSID / 密码 | 设备要连接的 2.4G Wi-Fi |
| 服务端地址 | 例如 `192.168.1.20:4318`，不要带 `http://` |
| DEVICE_ID | 状态通道，要和 hooks collector 里的 `AGENT_LIGHT_DEVICE_ID` 一致 |
| Device token | 服务端 `server status` 里看到的查询 token |

代码顶部仍保留默认占位配置：

```cpp
const char* DEFAULT_WIFI_SSID = "your-wifi-ssid";
const char* DEFAULT_WIFI_PASSWORD = "your-wifi-password";
const char* DEFAULT_SERVER_HOST = "your-server.example.com";
const char* DEFAULT_DEVICE_ID = "desk-light-01";
const char* DEFAULT_DEVICE_TOKEN = "replace-with-device-token";
```

这些默认值只是开发兜底；正常使用建议通过配网页保存到 NVS。

固件会自动把自己的显示设备带到查询参数：

```text
GET /api/devices/<DEVICE_ID>/status?displayId=<DISPLAY_ID>&layout=<LAYOUT>
```

## 连接失败与退避休眠

固件启动后会先读取 NVS 配置，再尝试连接 Wi-Fi。连接成功后会保存当前 AP 的 `BSSID/channel`，下次优先定向重连，速度更快。

如果已经有配置但启动时 Wi-Fi 连不上，设备不会一直耗电重试，而是按下面节奏 deep sleep：

```text
2 分钟 -> 4 分钟 -> 8 分钟 -> 16 分钟 -> 32 分钟 -> 64 分钟 -> 120 分钟
```

最大每 2 小时重试一次。deep sleep 会关闭 WS2812 和 Wi-Fi。到时间后自动唤醒重试。按键唤醒能力取决于当前 Arduino/ESP-IDF core 和所选 GPIO；如果按键唤醒不可用，定时唤醒仍然可用。

运行中如果连续 `OFFLINE_MS` 没成功查询到状态，会显示 `offline` 灯效；启动阶段 Wi-Fi 完全连不上时会直接进入退避休眠。

## 时钟同步

Wi-Fi 连接成功后，固件会参考 e-ink 项目的逻辑同步一次 NTP：

```cpp
configTime(8 * 3600, 0, "ntp.aliyun.com", "pool.ntp.org");
```

最多等待 5 秒。同步成功后串口会打印北京时间：

```text
[time] synced 2026-06-15 15:30:00
```

当前状态灯主要靠服务端时间戳，设备端同步时间主要用于后续日志、调试和未来本地策略。

## 8x8 推荐光效

8x8 是当前推荐主显示形态。固件里已经按这些方向实现了基础版本：

| intent | 8x8 效果 |
| --- | --- |
| `idle` | 青绿/蓝紫极光噪声云，中心慢呼吸 |
| `thinking` | 暖金核心 + 青蓝粒子漂移 |
| `busy` | 蓝紫数据流 + 白色数据包拖尾 |
| `approval` | 红橙边框能量门 + 中心白闪 |
| `offline` | 暗蓝灰单点休眠闪烁 |

后续可以继续按 README 里的 `aurora_core`、`quantum_drift`、`data_stream`、`alert_gate` 细化。
