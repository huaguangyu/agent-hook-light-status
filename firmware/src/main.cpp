// Agent Light WS2812 / WS2812B 固件。
//
// 一、切换开发板：
//   开发板不要在代码里改，去 platformio.ini 里用 env 切换。
//
//     pio run -e esp32-c3 -t upload
//     pio run -e esp32-wroom32e -t upload
//
//   esp32-c3       默认 DATA 脚是 GPIO8
//   esp32-wroom32e 默认 DATA 脚是 GPIO4
//
// 二、切换灯板：
//   灯板类型在本文件顶部改 LIGHT_PRESET。这样你同一块开发板可以快速换
//   8x8、4x4、2x2、12环、6灯条、单颗灯，不需要动 PlatformIO env。

#include <Arduino.h>
#include <WiFi.h>
#include <HTTPClient.h>
#include <ArduinoJson.h>
#include <FastLED.h>
#include <Preferences.h>
#include <WebServer.h>
#include <DNSServer.h>
#include <esp_sleep.h>
#include <esp_idf_version.h>
#include <sys/time.h>
#include <time.h>

// ===================== 用户配置：这里填 Wi-Fi 和服务端 =====================
// 注意：不要把真实 Wi-Fi 密码、线上 token 提交到 Git。
const char* DEFAULT_WIFI_SSID = "your-wifi-ssid";
const char* DEFAULT_WIFI_PASSWORD = "your-wifi-password";

// SERVER_HOST 只填 host[:port]，不要带 "http://"。
// 示例：
//   "192.168.1.20:4318"
//   "your-server.example.com"
const char* DEFAULT_SERVER_HOST = "your-server.example.com";

// DEVICE_ID 是“状态通道 id”，要和 hooks collector 里的 AGENT_LIGHT_DEVICE_ID 一致。
// 多个物理灯可以读取同一个 DEVICE_ID，只是 displayId/layout 不同。
const char* DEFAULT_DEVICE_ID = "desk-light-01";
const char* DEFAULT_DEVICE_TOKEN = "replace-with-device-token";

// POLL_MS：多久向服务端查询一次状态。
// OFFLINE_MS：连续多久没成功查询到状态，就进入 offline 灯效。
// WIFI_TIMEOUT_MS / WIFI_RETRIES：单次 Wi-Fi 连接超时和重试次数。
const int POLL_MS = 3000;
const int OFFLINE_MS = 30000;
const int WIFI_TIMEOUT_MS = 30000;
const int WIFI_RETRIES = 3;

// 配网页最多开启多久。超时没人配置，就休眠并按退避时间下次再试。
const int PORTAL_TIMEOUT_MS = 5 * 60 * 1000;

// 连接失败后的退避休眠。单位是分钟。
// 规则：2 -> 4 -> 8 -> 16 分钟指数增长，之后继续翻倍，最大 2 小时重试一次。
const int RETRY_SLEEP_MINUTES_BASE = 2;
const int RETRY_SLEEP_MINUTES_MAX = 120;
const long NTP_UTC_OFFSET = 8L * 3600L;

// ===================== 灯板切换：通常只改 LIGHT_PRESET 这一行 =====================
// 你手上换了不同 WS2812 灯板，就改下面这一行。
//
// 可选灯板：
//   PRESET_MATRIX_8X8  - 8x8 方阵，64 颗灯，推荐作为主显示
//   PRESET_MATRIX_4X4  - 4x4 方阵，16 颗灯
//   PRESET_MATRIX_2X2  - 2x2 方阵，4 颗灯
//   PRESET_RING_12     - 12 颗环形灯
//   PRESET_BAR_6       - 6 颗条形灯
//   PRESET_PIXEL_1     - 单颗 WS2812
//
// 如果矩阵显示方向不对，先不要改动画逻辑，改下面的 MATRIX_SERPENTINE
// 或 MATRIX_ORIGIN_TOP_LEFT 即可。
#define PRESET_MATRIX_8X8 1
#define PRESET_MATRIX_4X4 2
#define PRESET_MATRIX_2X2 3
#define PRESET_RING_12 4
#define PRESET_BAR_6 5
#define PRESET_PIXEL_1 6

// 默认使用 8x8 方阵。想换灯板时直接改这里：
//   #define LIGHT_PRESET PRESET_RING_12
//   #define LIGHT_PRESET PRESET_MATRIX_4X4
//   #define LIGHT_PRESET PRESET_BAR_6
#define LIGHT_PRESET PRESET_MATRIX_8X8

// WS2812 数据脚来自 platformio.ini：
//   esp32-c3       -> DEFAULT_LED_PIN=8
//   esp32-wroom32e -> DEFAULT_LED_PIN=4
//
// 如果某块板子的默认脚不方便，也可以临时在这里覆盖：
//   #undef DEFAULT_LED_PIN
//   #define DEFAULT_LED_PIN 5
#ifndef DEFAULT_LED_PIN
#define DEFAULT_LED_PIN 8
#endif

// 配网按键脚来自 platformio.ini：
//   esp32-c3       -> DEFAULT_CFG_BUTTON_PIN=9
//   esp32-wroom32e -> DEFAULT_CFG_BUTTON_PIN=0
//
// 接法：按钮一端接 GPIO，一端接 GND。代码使用 INPUT_PULLUP，按下时为 LOW。
#ifndef DEFAULT_CFG_BUTTON_PIN
#define DEFAULT_CFG_BUTTON_PIN 9
#endif

// 大多数 WS2812 / WS2812B 是 GRB + 800kHz。
// 如果你让它亮红色，结果亮成绿色，就把 GRB 改成 RGB。
#define LED_TYPE WS2812B
#define COLOR_ORDER GRB

// 矩阵走线方向配置。
// 很多 8x8 WS2812 板子是“蛇形走线”：第一行从左到右，第二行从右到左。
// 如果画面一行一行方向错乱，就在 0/1 之间切换 MATRIX_SERPENTINE。
//
// 如果画面上下颠倒，就在 0/1 之间切换 MATRIX_ORIGIN_TOP_LEFT。
#define MATRIX_SERPENTINE 1
#define MATRIX_ORIGIN_TOP_LEFT 1

#if LIGHT_PRESET == PRESET_MATRIX_8X8
static const char* DISPLAY_ID = "desk-matrix-8x8";
static const char* LAYOUT = "matrix8x8";
static const uint16_t LED_COUNT = 64;
static const uint8_t MATRIX_W = 8;
static const uint8_t MATRIX_H = 8;
#elif LIGHT_PRESET == PRESET_MATRIX_4X4
static const char* DISPLAY_ID = "desk-matrix-4x4";
static const char* LAYOUT = "matrix4x4";
static const uint16_t LED_COUNT = 16;
static const uint8_t MATRIX_W = 4;
static const uint8_t MATRIX_H = 4;
#elif LIGHT_PRESET == PRESET_MATRIX_2X2
static const char* DISPLAY_ID = "mini-2x2";
static const char* LAYOUT = "matrix2x2";
static const uint16_t LED_COUNT = 4;
static const uint8_t MATRIX_W = 2;
static const uint8_t MATRIX_H = 2;
#elif LIGHT_PRESET == PRESET_RING_12
static const char* DISPLAY_ID = "desk-ring-12";
static const char* LAYOUT = "ring12";
static const uint16_t LED_COUNT = 12;
static const uint8_t MATRIX_W = 0;
static const uint8_t MATRIX_H = 0;
#elif LIGHT_PRESET == PRESET_BAR_6
static const char* DISPLAY_ID = "bar-6";
static const char* LAYOUT = "bar6";
static const uint16_t LED_COUNT = 6;
static const uint8_t MATRIX_W = 0;
static const uint8_t MATRIX_H = 0;
#elif LIGHT_PRESET == PRESET_PIXEL_1
static const char* DISPLAY_ID = "single-dot";
static const char* LAYOUT = "pixel1";
static const uint16_t LED_COUNT = 1;
static const uint8_t MATRIX_W = 0;
static const uint8_t MATRIX_H = 0;
#else
#error "Unknown LIGHT_PRESET"
#endif

// 全局亮度上限。WS2812 满白电流很大，8x8 满白理论峰值可到数安。
// USB 调试阶段建议保守一些，避免电脑 USB 口、电线、灯板发热。
static const uint8_t MAX_BRIGHTNESS = 140;

CRGB leds[LED_COUNT];

// NVS 持久化配置。Preferences 会写入 ESP32 flash，断电后仍然保留。
// 配网页保存的 Wi-Fi、server、deviceId、token 都会落到这里。
static Preferences prefs;
static const char* NVS_NAMESPACE = "agentlight";
static const int CONFIG_VERSION = 1;
static const int CFG_MAX_SSID_LEN = 32;
static const int CFG_MAX_PASS_LEN = 64;
static const int CFG_MAX_HOST_LEN = 96;
static const int CFG_MAX_DEVICE_ID_LEN = 48;
static const int CFG_MAX_TOKEN_LEN = 160;

String cfgSSID;
String cfgPass;
String cfgServerHost;
String cfgDeviceID;
String cfgDeviceToken;
String cfgWiFiBSSID;
int cfgWiFiChannel = 0;

// RTC_DATA_ATTR 变量在 deep sleep 后仍然保留，用来记录连续失败次数。
RTC_DATA_ATTR int rtcRetryCount = 0;

static WebServer webServer(80);
static DNSServer dnsServer;
static bool portalActive = false;
static bool portalWifiConnecting = false;
static bool portalPendingRestart = false;
static unsigned long portalStartAt = 0;
static unsigned long portalRestartAt = 0;
static String portalLastError;

enum class Intent : uint8_t {
  Idle,
  Thinking,
  Busy,
  Approval,
  Offline
};

struct LightState {
  Intent intent = Intent::Offline;
  CRGB primary = CRGB(0x22, 0xC5, 0x5E);
  CRGB secondary = CRGB(0x14, 0xB8, 0xA6);
  uint8_t brightness = 45;
  uint8_t speed = 25;
  uint8_t density = 1;
};

LightState currentLight;

unsigned long lastPoll = 0;
unsigned long lastSuccess = 0;
unsigned long lastReconnect = 0;
uint16_t frameIndex = 0;
unsigned long buttonPressStart = 0;
bool buttonIgnoreUntilRelease = false;

String sanitizeText(String value, size_t maxLen, bool removeSpaces) {
  value = value.substring(0, maxLen);
  value.trim();
  value.replace("<", "");
  value.replace(">", "");
  value.replace("\"", "");
  value.replace("\r", "");
  value.replace("\n", "");
  value.replace("\t", "");
  if (removeSpaces) value.replace(" ", "");
  String cleaned;
  for (unsigned int i = 0; i < value.length(); i++) {
    char c = value.charAt(i);
    if (c >= 32 || (c & 0x80)) cleaned += c;
  }
  return cleaned;
}

String normalizeHost(String value) {
  value = sanitizeText(value, CFG_MAX_HOST_LEN, true);
  if (value.startsWith("http://")) value.remove(0, 7);
  if (value.startsWith("https://")) value.remove(0, 8);
  while (value.endsWith("/") && value.length() > 0) {
    value.remove(value.length() - 1);
  }
  return value;
}

String jsonEscape(const String& input) {
  String out;
  out.reserve(input.length() + 8);
  for (unsigned int i = 0; i < input.length(); i++) {
    char c = input.charAt(i);
    if (c == '"' || c == '\\') out += '\\';
    out += c;
  }
  return out;
}

bool hasUsableConfig() {
  return cfgSSID.length() > 0 &&
         cfgServerHost.length() > 0 &&
         cfgDeviceID.length() > 0 &&
         cfgDeviceToken.length() > 0;
}

void loadConfig() {
  prefs.begin(NVS_NAMESPACE, true);
  int version = prefs.getInt("version", 0);
  if (version != CONFIG_VERSION) {
    prefs.end();
    cfgSSID = DEFAULT_WIFI_SSID;
    cfgPass = DEFAULT_WIFI_PASSWORD;
    cfgServerHost = normalizeHost(DEFAULT_SERVER_HOST);
    cfgDeviceID = DEFAULT_DEVICE_ID;
    cfgDeviceToken = DEFAULT_DEVICE_TOKEN;
    cfgWiFiBSSID = "";
    cfgWiFiChannel = 0;
    if (cfgSSID.startsWith("your-") || cfgDeviceToken.startsWith("replace-")) {
      cfgSSID = "";
      cfgPass = "";
      cfgServerHost = "";
      cfgDeviceID = "";
      cfgDeviceToken = "";
    }
    return;
  }

  cfgSSID = prefs.getString("ssid", "");
  cfgPass = prefs.getString("pass", "");
  cfgServerHost = normalizeHost(prefs.getString("server", ""));
  cfgDeviceID = sanitizeText(prefs.getString("device_id", ""), CFG_MAX_DEVICE_ID_LEN, true);
  cfgDeviceToken = sanitizeText(prefs.getString("token", ""), CFG_MAX_TOKEN_LEN, true);
  cfgWiFiBSSID = prefs.getString("wifi_bssid", "");
  cfgWiFiChannel = prefs.getInt("wifi_channel", 0);
  prefs.end();
}

void saveConfig(const String& ssid, const String& pass, const String& serverHost, const String& deviceID, const String& token) {
  prefs.begin(NVS_NAMESPACE, false);
  prefs.putInt("version", CONFIG_VERSION);
  prefs.putString("ssid", ssid);
  prefs.putString("pass", pass);
  prefs.putString("server", normalizeHost(serverHost));
  prefs.putString("device_id", sanitizeText(deviceID, CFG_MAX_DEVICE_ID_LEN, true));
  prefs.putString("token", sanitizeText(token, CFG_MAX_TOKEN_LEN, true));
  prefs.putString("wifi_bssid", "");
  prefs.putInt("wifi_channel", 0);
  prefs.end();

  cfgSSID = ssid;
  cfgPass = pass;
  cfgServerHost = normalizeHost(serverHost);
  cfgDeviceID = sanitizeText(deviceID, CFG_MAX_DEVICE_ID_LEN, true);
  cfgDeviceToken = sanitizeText(token, CFG_MAX_TOKEN_LEN, true);
  cfgWiFiBSSID = "";
  cfgWiFiChannel = 0;
}

void saveWiFiApInfo(const String& bssid, int channel) {
  if (bssid.length() == 0 || channel <= 0) return;
  if (cfgWiFiBSSID == bssid && cfgWiFiChannel == channel) return;
  prefs.begin(NVS_NAMESPACE, false);
  prefs.putInt("version", CONFIG_VERSION);
  prefs.putString("wifi_bssid", bssid);
  prefs.putInt("wifi_channel", channel);
  prefs.end();
  cfgWiFiBSSID = bssid;
  cfgWiFiChannel = channel;
}

bool parseBSSID(const String& text, uint8_t out[6]) {
  if (text.length() != 17) return false;
  int values[6];
  if (sscanf(text.c_str(), "%x:%x:%x:%x:%x:%x",
             &values[0], &values[1], &values[2],
             &values[3], &values[4], &values[5]) != 6) {
    return false;
  }
  for (int i = 0; i < 6; i++) {
    if (values[i] < 0 || values[i] > 255) return false;
    out[i] = (uint8_t)values[i];
  }
  return true;
}

int nextRetrySleepMinutes() {
  int failures = rtcRetryCount;
  if (failures < 1) failures = 1;

  int minutes = RETRY_SLEEP_MINUTES_BASE;
  for (int i = 1; i < failures; i++) {
    if (minutes >= RETRY_SLEEP_MINUTES_MAX) return RETRY_SLEEP_MINUTES_MAX;
    minutes *= 2;
  }
  if (minutes > RETRY_SLEEP_MINUTES_MAX) minutes = RETRY_SLEEP_MINUTES_MAX;
  return minutes;
}

void enterDeepSleepMinutes(int minutes) {
  if (minutes < 1) minutes = 1;
  if (minutes > RETRY_SLEEP_MINUTES_MAX) minutes = RETRY_SLEEP_MINUTES_MAX;
  Serial.printf("[sleep] deep sleep %dmin, retry=%d\n", minutes, rtcRetryCount);
  FastLED.clear(true);
  WiFi.disconnect(true);
  WiFi.mode(WIFI_OFF);
  esp_sleep_enable_timer_wakeup((uint64_t)minutes * 60ULL * 1000000ULL);
#if ESP_IDF_VERSION_MAJOR >= 5
  esp_deep_sleep_enable_gpio_wakeup((1ULL << DEFAULT_CFG_BUTTON_PIN), ESP_GPIO_WAKEUP_GPIO_LOW);
#elif defined(CONFIG_IDF_TARGET_ESP32)
  esp_sleep_enable_ext0_wakeup((gpio_num_t)DEFAULT_CFG_BUTTON_PIN, 0);
#else
  Serial.println(F("[sleep] button wake is not enabled on this Arduino core; timer wake still works"));
#endif
  esp_deep_sleep_start();
}

bool syncTimeFromNTP() {
  Serial.println(F("[time] sync via ntp.aliyun.com"));
  configTime(NTP_UTC_OFFSET, 0, "ntp.aliyun.com", "pool.ntp.org");

  for (int i = 0; i < 50; i++) {
    struct timeval tv;
    gettimeofday(&tv, nullptr);
    if (tv.tv_sec > 1700000000) {
      struct tm* tmInfo = localtime(&tv.tv_sec);
      Serial.printf("[time] synced %04d-%02d-%02d %02d:%02d:%02d\n",
                    1900 + tmInfo->tm_year,
                    1 + tmInfo->tm_mon,
                    tmInfo->tm_mday,
                    tmInfo->tm_hour,
                    tmInfo->tm_min,
                    tmInfo->tm_sec);
      return true;
    }
    delay(100);
  }

  Serial.println(F("[time] ntp sync timed out"));
  return false;
}

void beginButton() {
  pinMode(DEFAULT_CFG_BUTTON_PIN, INPUT_PULLUP);
}

bool buttonHeldAtBoot() {
  if (digitalRead(DEFAULT_CFG_BUTTON_PIN) != LOW) return false;
  delay(400);
  return digitalRead(DEFAULT_CFG_BUTTON_PIN) == LOW;
}

bool buttonVeryLongPressed() {
  bool pressed = digitalRead(DEFAULT_CFG_BUTTON_PIN) == LOW;
  if (buttonIgnoreUntilRelease) {
    if (!pressed) buttonIgnoreUntilRelease = false;
    buttonPressStart = 0;
    return false;
  }
  if (!pressed) {
    buttonPressStart = 0;
    return false;
  }
  if (buttonPressStart == 0) buttonPressStart = millis();
  if (millis() - buttonPressStart >= 8000UL) {
    buttonPressStart = 0;
    buttonIgnoreUntilRelease = true;
    return true;
  }
  return false;
}

const char* wifiStatusName(wl_status_t status) {
  switch (status) {
    case WL_IDLE_STATUS: return "IDLE";
    case WL_NO_SSID_AVAIL: return "NO_SSID";
    case WL_SCAN_COMPLETED: return "SCAN_DONE";
    case WL_CONNECTED: return "CONNECTED";
    case WL_CONNECT_FAILED: return "CONNECT_FAILED";
    case WL_CONNECTION_LOST: return "CONNECTION_LOST";
    case WL_DISCONNECTED: return "DISCONNECTED";
    default: return "UNKNOWN";
  }
}

uint16_t xy(uint8_t x, uint8_t y) {
  if (MATRIX_W == 0 || MATRIX_H == 0 || x >= MATRIX_W || y >= MATRIX_H) return 0;

#if !MATRIX_ORIGIN_TOP_LEFT
  y = MATRIX_H - 1 - y;
#endif

#if MATRIX_SERPENTINE
  if (y & 1) {
    return y * MATRIX_W + (MATRIX_W - 1 - x);
  }
#endif
  return y * MATRIX_W + x;
}

CRGB parseHexColor(const char* value, const CRGB& fallback) {
  if (!value || value[0] != '#' || strlen(value) != 7) return fallback;
  char* end = nullptr;
  uint32_t rgb = strtoul(value + 1, &end, 16);
  if (!end || *end != '\0') return fallback;
  return CRGB((rgb >> 16) & 0xFF, (rgb >> 8) & 0xFF, rgb & 0xFF);
}

Intent parseIntent(const char* value) {
  if (!value) return Intent::Idle;
  if (strcmp(value, "thinking") == 0) return Intent::Thinking;
  if (strcmp(value, "busy") == 0) return Intent::Busy;
  if (strcmp(value, "approval") == 0) return Intent::Approval;
  if (strcmp(value, "idle") == 0) return Intent::Idle;
  return Intent::Idle;
}

void setOfflineState() {
  currentLight.intent = Intent::Offline;
  currentLight.primary = CRGB(0x33, 0x41, 0x55);
  currentLight.secondary = CRGB(0x94, 0xA3, 0xB8);
  currentLight.brightness = 35;
  currentLight.speed = 20;
  currentLight.density = 1;
}

void setFallbackFromColorEffect(const char* color, const char* effect) {
  if (strcmp(color, "green") == 0) {
    currentLight.intent = Intent::Idle;
    currentLight.primary = CRGB(0x22, 0xC5, 0x5E);
    currentLight.secondary = CRGB(0x14, 0xB8, 0xA6);
    currentLight.brightness = 45;
    currentLight.speed = 25;
    currentLight.density = 1;
  } else if (strcmp(color, "yellow") == 0) {
    currentLight.intent = Intent::Thinking;
    currentLight.primary = CRGB(0xFA, 0xCC, 0x15);
    currentLight.secondary = CRGB(0x38, 0xBD, 0xF8);
    currentLight.brightness = 95;
    currentLight.speed = 55;
    currentLight.density = 1;
  } else if (strcmp(color, "red") == 0 && strcmp(effect, "fast_blink") == 0) {
    currentLight.intent = Intent::Approval;
    currentLight.primary = CRGB(0xEF, 0x44, 0x44);
    currentLight.secondary = CRGB(0xF9, 0x73, 0x16);
    currentLight.brightness = 190;
    currentLight.speed = 140;
    currentLight.density = 4;
  } else {
    currentLight.intent = Intent::Busy;
    currentLight.primary = CRGB(0x3B, 0x82, 0xF6);
    currentLight.secondary = CRGB(0x8B, 0x5C, 0xF6);
    currentLight.brightness = 120;
    currentLight.speed = 90;
    currentLight.density = 3;
  }
}

void applyStatusJson(JsonDocument& doc) {
  const char* color = doc["color"] | "";
  const char* effect = doc["effect"] | "";
  JsonVariant light = doc["light"];

  if (light.is<JsonObject>()) {
    currentLight.intent = parseIntent(light["intent"] | "idle");
    currentLight.primary = parseHexColor(light["primary"] | "", CRGB(0x22, 0xC5, 0x5E));
    currentLight.secondary = parseHexColor(light["secondary"] | "", CRGB(0x14, 0xB8, 0xA6));
    currentLight.brightness = constrain((int)(light["brightness"] | 80), 1, 255);
    currentLight.speed = constrain((int)(light["speed"] | 50), 1, 255);
    currentLight.density = constrain((int)(light["density"] | 1), 1, 8);
  } else {
    setFallbackFromColorEffect(color, effect);
  }

  currentLight.brightness = min(currentLight.brightness, MAX_BRIGHTNESS);
  Serial.printf("[poll] intent=%d bri=%u speed=%u density=%u\n",
                (int)currentLight.intent,
                currentLight.brightness,
                currentLight.speed,
                currentLight.density);
}

void fadeAll(uint8_t amount) {
  for (uint16_t i = 0; i < LED_COUNT; i++) {
    leds[i].fadeToBlackBy(amount);
  }
}

void renderMatrix8x8(const LightState& light, unsigned long now) {
  uint8_t t = now / max(8, 260 - light.speed);
  fadeAll(28);

  switch (light.intent) {
    case Intent::Idle:
      for (uint8_t y = 0; y < MATRIX_H; y++) {
        for (uint8_t x = 0; x < MATRIX_W; x++) {
          uint8_t n = inoise8(x * 36, y * 36, t * 4);
          CRGB c = blend(CRGB(4, 8, 24), blend(light.secondary, light.primary, n), 90);
          c.nscale8_video(scale8(n, 120));
          leds[xy(x, y)] += c;
        }
      }
      break;

    case Intent::Thinking: {
      uint8_t pulse = beatsin8(18, 80, 210);
      for (uint8_t y = 0; y < MATRIX_H; y++) {
        for (uint8_t x = 0; x < MATRIX_W; x++) {
          int dx = (int)x - 3;
          int dy = (int)y - 3;
          uint8_t dist = min(255, (dx * dx + dy * dy) * 28);
          CRGB c = blend(light.primary, light.secondary, dist);
          c.nscale8_video(qsub8(pulse, dist / 2));
          leds[xy(x, y)] += c;
        }
      }
      for (uint8_t i = 0; i < light.density + 2; i++) {
        uint8_t x = (inoise8(t * 5, i * 70) >> 5) & 7;
        uint8_t y = (inoise8(i * 80, t * 5) >> 5) & 7;
        leds[xy(x, y)] += CRGB(120, 220, 255);
      }
      break;
    }

    case Intent::Busy: {
      uint8_t offset = t / 3;
      for (uint8_t y = 0; y < MATRIX_H; y++) {
        uint8_t x = (offset + y * 2) & 7;
        leds[xy(x, y)] += light.primary;
        leds[xy((x + 7) & 7, y)] += light.secondary;
        if (((offset + y) % 5) == 0) leds[xy(x, y)] += CRGB::White;
      }
      break;
    }

    case Intent::Approval: {
      bool flash = ((now / 130) % 2) == 0;
      CRGB edge = flash ? light.primary : light.secondary;
      for (uint8_t i = 0; i < 8; i++) {
        leds[xy(i, 0)] = edge;
        leds[xy(i, 7)] = edge;
        leds[xy(0, i)] = edge;
        leds[xy(7, i)] = edge;
      }
      if (flash) {
        leds[xy(3, 3)] = CRGB::White;
        leds[xy(4, 3)] = CRGB::White;
        leds[xy(3, 4)] = CRGB::White;
        leds[xy(4, 4)] = CRGB::White;
      }
      break;
    }

    case Intent::Offline: {
      uint8_t pulse = beatsin8(8, 10, 90);
      leds[xy(0, 0)] = light.secondary;
      leds[xy(0, 0)].nscale8_video(pulse);
      break;
    }
  }
}

void renderMatrixSmall(const LightState& light, unsigned long now) {
  fadeAll(35);
  uint8_t pulse = beatsin8(18, 40, light.brightness);

  if (light.intent == Intent::Approval) {
    CRGB c = ((now / 130) % 2) ? light.primary : light.secondary;
    fill_solid(leds, LED_COUNT, c);
    return;
  }

  if (MATRIX_W > 0 && MATRIX_H > 0) {
    uint8_t step = (now / max(80, 330 - light.speed)) % LED_COUNT;
    for (uint8_t y = 0; y < MATRIX_H; y++) {
      for (uint8_t x = 0; x < MATRIX_W; x++) {
        uint16_t index = xy(x, y);
        CRGB c = light.intent == Intent::Busy ? light.secondary : light.primary;
        c.nscale8_video(25);
        leds[index] += c;
      }
    }
    leds[step] += light.intent == Intent::Thinking ? light.primary : light.secondary;
  } else {
    fill_solid(leds, LED_COUNT, light.primary);
  }
  for (uint16_t i = 0; i < LED_COUNT; i++) leds[i].nscale8_video(pulse);
}

void renderLinearOrRing(const LightState& light, unsigned long now) {
  fadeAll(light.intent == Intent::Approval ? 70 : 32);
  uint8_t speed = max<uint8_t>(1, light.speed);
  uint8_t head = (now / max(35, 280 - speed)) % LED_COUNT;

  if (light.intent == Intent::Idle) {
    for (uint16_t i = 0; i < LED_COUNT; i++) {
      CRGB c = blend(light.primary, light.secondary, sin8(i * 28 + now / 30));
      c.nscale8_video(55);
      leds[i] += c;
    }
    return;
  }

  if (light.intent == Intent::Approval) {
    CRGB c = ((now / 130) % 2) ? light.primary : light.secondary;
    fill_solid(leds, LED_COUNT, c);
    return;
  }

  uint8_t dots = light.intent == Intent::Busy ? min<uint8_t>(4, light.density) : 1;
  for (uint8_t d = 0; d < dots; d++) {
    uint16_t pos = (head + d * (LED_COUNT / max<uint8_t>(1, dots))) % LED_COUNT;
    leds[pos] += d == 0 ? light.primary : light.secondary;
    leds[(pos + LED_COUNT - 1) % LED_COUNT] += light.secondary;
  }
}

void renderLeds() {
  FastLED.setBrightness(currentLight.brightness);

#if LIGHT_PRESET == PRESET_MATRIX_8X8
  renderMatrix8x8(currentLight, millis());
#elif LIGHT_PRESET == PRESET_MATRIX_4X4 || LIGHT_PRESET == PRESET_MATRIX_2X2
  renderMatrixSmall(currentLight, millis());
#else
  renderLinearOrRing(currentLight, millis());
#endif

  FastLED.show();
  frameIndex++;
}

void printMatchingNetworks() {
  Serial.printf("[wifi] scanning for SSID \"%s\" ...\n", cfgSSID.c_str());
  int count = WiFi.scanNetworks();
  if (count <= 0) {
    Serial.println(F("[wifi] scan found no networks"));
    return;
  }

  bool found = false;
  for (int i = 0; i < count; i++) {
    if (WiFi.SSID(i) == cfgSSID) {
      found = true;
      Serial.printf("[wifi] found ssid=%s rssi=%d channel=%d bssid=%s\n",
                    WiFi.SSID(i).c_str(),
                    WiFi.RSSI(i),
                    WiFi.channel(i),
                    WiFi.BSSIDstr(i).c_str());
    }
  }
  if (!found) {
    Serial.printf("[wifi] SSID \"%s\" not visible in %d scanned networks\n", cfgSSID.c_str(), count);
  }
  WiFi.scanDelete();
}

bool connectWifi(bool scanOnFailure) {
  if (cfgSSID.length() == 0) return false;
  if (WiFi.status() == WL_CONNECTED) return true;

  for (int retry = 1; retry <= WIFI_RETRIES; retry++) {
    Serial.printf("[wifi] connecting, attempt=%d/%d ssid=%s\n", retry, WIFI_RETRIES, cfgSSID.c_str());

    WiFi.disconnect(true, false);
    WiFi.mode(WIFI_OFF);
    delay(300);
    WiFi.mode(WIFI_STA);
    WiFi.setSleep(false);
    WiFi.setTxPower(WIFI_POWER_19_5dBm);
    WiFi.setHostname("agent-light");

    uint8_t bssid[6];
    if (cfgWiFiChannel > 0 && parseBSSID(cfgWiFiBSSID, bssid)) {
      Serial.printf("[wifi] directed reconnect channel=%d bssid=%s\n", cfgWiFiChannel, cfgWiFiBSSID.c_str());
      WiFi.begin(cfgSSID.c_str(), cfgPass.c_str(), cfgWiFiChannel, bssid);
    } else {
      WiFi.begin(cfgSSID.c_str(), cfgPass.c_str());
    }

    unsigned long started = millis();
    wl_status_t lastStatus = WiFi.status();
    Serial.printf("[wifi] status=%s\n", wifiStatusName(lastStatus));

    while (WiFi.status() != WL_CONNECTED && millis() - started < (unsigned long)WIFI_TIMEOUT_MS) {
      wl_status_t status = WiFi.status();
      if (status != lastStatus) {
        lastStatus = status;
        Serial.printf("[wifi] status=%s\n", wifiStatusName(status));
      }
      renderLeds();
      delay(20);
    }

    if (WiFi.status() == WL_CONNECTED) {
      Serial.printf("[wifi] connected, IP=%s RSSI=%d channel=%d BSSID=%s\n",
                    WiFi.localIP().toString().c_str(),
                    WiFi.RSSI(),
                    WiFi.channel(),
                    WiFi.BSSIDstr().c_str());
      saveWiFiApInfo(WiFi.BSSIDstr(), WiFi.channel());
      return true;
    }

    Serial.printf("[wifi] failed, final status=%s\n", wifiStatusName(WiFi.status()));
    WiFi.disconnect(true, false);
    delay(1200);
  }

  if (scanOnFailure) printMatchingNetworks();
  return false;
}

bool poll() {
  WiFiClient client;
  HTTPClient http;
  String url = String("http://") + cfgServerHost +
               "/api/devices/" + cfgDeviceID +
               "/status?displayId=" + DISPLAY_ID +
               "&layout=" + LAYOUT;

  if (!http.begin(client, url)) {
    Serial.println(F("[poll] http.begin failed"));
    return false;
  }
  http.setConnectTimeout(2000);
  http.setTimeout(3000);
  http.addHeader(F("Authorization"), String("Bearer ") + cfgDeviceToken);

  int code = http.GET();
  if (code != 200) {
    Serial.printf("[poll] HTTP %d\n", code);
    http.end();
    return false;
  }

  String body = http.getString();
  http.end();

  JsonDocument doc;
  DeserializationError err = deserializeJson(doc, body);
  if (err) {
    Serial.printf("[poll] json parse failed: %s\n", err.c_str());
    return false;
  }

  applyStatusJson(doc);
  return true;
}

void ensureWifi(unsigned long now) {
  if (WiFi.status() == WL_CONNECTED) return;
  if (now - lastReconnect < 15000) return;
  lastReconnect = now;
  connectWifi(false);
}

String portalHtml() {
  return R"HTML(
<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>Agent Light 配网</title>
  <style>
    body{margin:0;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#0b1020;color:#e5eefb}
    main{max-width:560px;margin:0 auto;padding:28px 18px}
    h1{font-size:24px;margin:0 0 8px}
    p{color:#9fb0c7;font-size:14px;line-height:1.6}
    label{display:block;margin:16px 0 6px;color:#c8d6ea;font-size:13px}
    input,select{box-sizing:border-box;width:100%;border:1px solid #26344f;background:#111a2e;color:#f8fbff;border-radius:8px;padding:11px;font-size:15px}
    button{border:0;border-radius:8px;background:#38bdf8;color:#07111f;padding:11px 16px;font-weight:700;margin-top:18px;cursor:pointer}
    button.secondary{background:#1f2a44;color:#dbeafe;margin-left:8px}
    .row{display:flex;gap:8px;align-items:center}.row select{flex:1}.row button{margin-top:0}
    .hint{font-size:12px;color:#8ca0bd}.status{margin-top:16px;padding:12px;border-radius:8px;background:#111a2e;color:#bfdbfe;white-space:pre-wrap}
  </style>
</head>
<body>
<main>
  <h1>Agent Light 配网</h1>
  <p>连接这个热点后，填写 Wi-Fi、Agent Light 服务端地址、状态通道和查询 token。保存成功后设备会自动重启。</p>
  <label>Wi-Fi 名称</label>
  <div class="row">
    <select id="ssidSelect"><option value="">扫描中...</option></select>
    <button class="secondary" onclick="scan()">扫描</button>
  </div>
  <input id="ssid" placeholder="也可以手动输入 SSID">
  <label>Wi-Fi 密码</label>
  <input id="pass" type="password" placeholder="开放网络可留空">
  <label>服务端地址</label>
  <input id="server" placeholder="例如 192.168.1.20:4318，不要带 http://">
  <div class="hint">当前固件使用明文 HTTP；SERVER_HOST 只填 host[:port]。</div>
  <label>状态通道 DEVICE_ID</label>
  <input id="deviceId" placeholder="例如 workspace 或 desk-light-01">
  <label>Device token</label>
  <input id="token" placeholder="服务端 server status 里看到的 Device token">
  <button onclick="save()">连接并保存</button>
  <button class="secondary" onclick="restart()">立即重启</button>
  <div id="status" class="status">等待操作...</div>
</main>
<script>
function setStatus(t){document.getElementById('status').textContent=t}
async function loadInfo(){
  const r=await fetch('/info'); const d=await r.json();
  ssid.value=d.ssid||''; pass.value=d.pass||''; server.value=d.server||''; deviceId.value=d.device_id||''; token.value=d.token||'';
}
async function scan(){
  setStatus('正在扫描 Wi-Fi...');
  const r=await fetch('/scan'); const d=await r.json();
  ssidSelect.innerHTML='<option value="">选择扫描到的 Wi-Fi</option>';
  (d.networks||[]).forEach(n=>{
    const o=document.createElement('option');
    o.value=n.ssid; o.textContent=n.ssid+'  RSSI '+n.rssi+(n.secure?'  加密':'  开放');
    ssidSelect.appendChild(o);
  });
  setStatus('扫描完成');
}
ssidSelect.onchange=()=>{ if(ssidSelect.value) ssid.value=ssidSelect.value }
async function save(){
  const fd=new FormData();
  fd.append('ssid',ssid.value); fd.append('pass',pass.value); fd.append('server',server.value);
  fd.append('device_id',deviceId.value); fd.append('token',token.value);
  setStatus('正在连接并保存...');
  const r=await fetch('/save',{method:'POST',body:fd}); const d=await r.json();
  if(d.ok) setStatus('保存成功，设备即将重启。');
  else setStatus('失败：'+(d.msg||'unknown'));
}
async function restart(){ await fetch('/restart',{method:'POST'}); setStatus('正在重启...') }
loadInfo(); scan();
</script>
</body>
</html>
)HTML";
}

void renderPortalLed() {
  uint8_t phase = (millis() - portalStartAt) % 1800;
  fadeAll(35);
  CRGB c = phase < 900 ? CRGB(0x38, 0xBD, 0xF8) : CRGB(0xA7, 0x8B, 0xFA);
  if (LED_COUNT == 1) {
    leds[0] = c;
  } else {
    uint16_t pos = (millis() / 120) % LED_COUNT;
    leds[pos] += c;
    leds[(pos + LED_COUNT - 1) % LED_COUNT] += CRGB(20, 60, 120);
  }
  FastLED.setBrightness(80);
  FastLED.show();
}

void startPortal() {
  Serial.println(F("[portal] starting AP config portal"));
  WiFi.disconnect(true);
  WiFi.mode(WIFI_OFF);
  delay(200);

  String mac = WiFi.macAddress();
  String apName = "AgentLight-" + mac.substring(mac.length() - 5);
  apName.replace(":", "");

  WiFi.mode(WIFI_AP_STA);
  WiFi.softAP(apName.c_str());
  delay(100);
  Serial.printf("[portal] AP=%s IP=%s\n", apName.c_str(), WiFi.softAPIP().toString().c_str());

  dnsServer.start(53, "*", WiFi.softAPIP());

  webServer.on("/", HTTP_GET, []() {
    webServer.send(200, "text/html; charset=utf-8", portalHtml());
  });

  webServer.on("/info", HTTP_GET, []() {
    String json = "{\"mac\":\"" + WiFi.macAddress() +
                  "\",\"ssid\":\"" + jsonEscape(cfgSSID) +
                  "\",\"pass\":\"" + jsonEscape(cfgPass) +
                  "\",\"server\":\"" + jsonEscape(cfgServerHost) +
                  "\",\"device_id\":\"" + jsonEscape(cfgDeviceID) +
                  "\",\"token\":\"" + jsonEscape(cfgDeviceToken) + "\"}";
    webServer.send(200, "application/json", json);
  });

  webServer.on("/status", HTTP_GET, []() {
    String json = "{\"state\":\"";
    if (WiFi.status() == WL_CONNECTED) {
      json += "connected\",\"ip\":\"" + WiFi.localIP().toString() + "\"";
    } else if (portalWifiConnecting) {
      json += "connecting\"";
    } else if (portalLastError.length() > 0) {
      json += "failed\",\"error\":\"" + jsonEscape(portalLastError) + "\"";
    } else {
      json += "idle\"";
    }
    json += "}";
    webServer.send(200, "application/json", json);
  });

  webServer.on("/scan", HTTP_GET, []() {
    int n = WiFi.scanNetworks();
    struct NetInfo { String ssid; int rssi; bool secure; };
    NetInfo best[32];
    int count = 0;
    for (int i = 0; i < n; i++) {
      String ssid = WiFi.SSID(i);
      if (ssid.length() == 0) continue;
      int rssi = WiFi.RSSI(i);
      bool secure = WiFi.encryptionType(i) != WIFI_AUTH_OPEN;
      int found = -1;
      for (int j = 0; j < count; j++) {
        if (best[j].ssid == ssid) {
          found = j;
          break;
        }
      }
      if (found >= 0) {
        if (rssi > best[found].rssi) {
          best[found].rssi = rssi;
          best[found].secure = secure;
        }
      } else if (count < 32) {
        best[count++] = {ssid, rssi, secure};
      }
    }
    for (int i = 0; i < count - 1; i++) {
      for (int j = i + 1; j < count; j++) {
        if (best[j].rssi > best[i].rssi) {
          NetInfo tmp = best[i];
          best[i] = best[j];
          best[j] = tmp;
        }
      }
    }
    String json = "{\"networks\":[";
    for (int i = 0; i < count; i++) {
      if (i > 0) json += ",";
      json += "{\"ssid\":\"" + jsonEscape(best[i].ssid) +
              "\",\"rssi\":" + String(best[i].rssi) +
              ",\"secure\":" + String(best[i].secure ? "true" : "false") + "}";
    }
    json += "]}";
    webServer.send(200, "application/json", json);
  });

  webServer.on("/save", HTTP_POST, []() {
    String ssid = sanitizeText(webServer.arg("ssid"), CFG_MAX_SSID_LEN, false);
    String pass = sanitizeText(webServer.arg("pass"), CFG_MAX_PASS_LEN, false);
    String serverHost = normalizeHost(webServer.arg("server"));
    String deviceID = sanitizeText(webServer.arg("device_id"), CFG_MAX_DEVICE_ID_LEN, true);
    String token = sanitizeText(webServer.arg("token"), CFG_MAX_TOKEN_LEN, true);

    if (ssid.length() == 0) {
      webServer.send(200, "application/json", "{\"ok\":false,\"msg\":\"SSID 不能为空\"}");
      return;
    }
    if (serverHost.length() == 0 || deviceID.length() == 0 || token.length() == 0) {
      webServer.send(200, "application/json", "{\"ok\":false,\"msg\":\"服务端、DEVICE_ID、token 都必须填写\"}");
      return;
    }
    if (pass.length() > 0 && pass.length() < 8) {
      webServer.send(200, "application/json", "{\"ok\":false,\"msg\":\"Wi-Fi 密码少于 8 位；开放网络请留空\"}");
      return;
    }

    portalWifiConnecting = true;
    portalLastError = "";
    WiFi.mode(WIFI_AP_STA);
    WiFi.begin(ssid.c_str(), pass.c_str());
    unsigned long started = millis();
    while (WiFi.status() != WL_CONNECTED && millis() - started < (unsigned long)WIFI_TIMEOUT_MS) {
      renderPortalLed();
      delay(30);
    }
    portalWifiConnecting = false;

    if (WiFi.status() == WL_CONNECTED) {
      saveConfig(ssid, pass, serverHost, deviceID, token);
      saveWiFiApInfo(WiFi.BSSIDstr(), WiFi.channel());
      rtcRetryCount = 0;
      webServer.send(200, "application/json", "{\"ok\":true}");
      portalPendingRestart = true;
      portalRestartAt = millis() + 2000;
      return;
    }

    portalLastError = "Wi-Fi 连接失败，请检查密码或信号";
    WiFi.disconnect(false);
    WiFi.mode(WIFI_AP_STA);
    webServer.send(200, "application/json", "{\"ok\":false,\"msg\":\"Wi-Fi 连接失败\"}");
  });

  webServer.on("/restart", HTTP_POST, []() {
    webServer.send(200, "application/json", "{\"ok\":true}");
    delay(300);
    ESP.restart();
  });

  webServer.onNotFound([]() {
    String path = webServer.uri();
    if (path == "/generate_204" || path == "/gen_204" || path == "/hotspot-detect.html") {
      webServer.send(204);
      return;
    }
    webServer.sendHeader("Location", "http://" + WiFi.softAPIP().toString());
    webServer.send(302, "text/plain", "");
  });

  webServer.begin();
  portalStartAt = millis();
  portalActive = true;
  buttonIgnoreUntilRelease = true;
}

void handlePortal() {
  dnsServer.processNextRequest();
  webServer.handleClient();
  renderPortalLed();

  if (portalPendingRestart && millis() >= portalRestartAt) {
    delay(100);
    ESP.restart();
  }

  if (millis() - portalStartAt > (unsigned long)PORTAL_TIMEOUT_MS) {
    rtcRetryCount++;
    enterDeepSleepMinutes(nextRetrySleepMinutes());
  }
}

void selfTest() {
  fill_solid(leds, LED_COUNT, CRGB::Red);
  FastLED.show();
  delay(250);
  fill_solid(leds, LED_COUNT, CRGB::Green);
  FastLED.show();
  delay(250);
  fill_solid(leds, LED_COUNT, CRGB::Blue);
  FastLED.show();
  delay(250);
  FastLED.clear(true);
}

void setup() {
  beginButton();
  Serial.begin(115200);
  delay(200);
  Serial.println(F("\n== Agent Light WS2812 boot =="));
  Serial.printf("[config] pin=%d count=%u display=%s layout=%s\n",
                DEFAULT_LED_PIN,
                LED_COUNT,
                DISPLAY_ID,
                LAYOUT);

  FastLED.addLeds<LED_TYPE, DEFAULT_LED_PIN, COLOR_ORDER>(leds, LED_COUNT);
  FastLED.setCorrection(TypicalLEDStrip);
  FastLED.setDither(true);
  FastLED.setBrightness(MAX_BRIGHTNESS);
  FastLED.clear(true);
  setOfflineState();
  selfTest();

  loadConfig();
  WiFi.persistent(false);

  bool forcePortal = buttonHeldAtBoot();
  if (forcePortal) {
    Serial.println(F("[button] held at boot -> portal"));
  }

  if (forcePortal || !hasUsableConfig()) {
    Serial.println(F("[config] missing config or forced portal"));
    startPortal();
    return;
  }

  if (!connectWifi(true)) {
    rtcRetryCount++;
    int sleepMinutes = nextRetrySleepMinutes();
    Serial.printf("[wifi] connect failed, retry sleep=%dmin retry=%d\n", sleepMinutes, rtcRetryCount);
    enterDeepSleepMinutes(sleepMinutes);
    return;
  }
  rtcRetryCount = 0;
  syncTimeFromNTP();
  lastSuccess = millis();
}

void loop() {
  if (portalActive) {
    handlePortal();
    delay(20);
    return;
  }

  unsigned long now = millis();

  if (buttonVeryLongPressed()) {
    Serial.println(F("[button] very long press -> portal"));
    startPortal();
    return;
  }

  ensureWifi(now);

  if (WiFi.status() == WL_CONNECTED && now - lastPoll >= (unsigned long)POLL_MS) {
    lastPoll = now;
    if (poll()) lastSuccess = now;
  }

  if (now - lastSuccess > (unsigned long)OFFLINE_MS) {
    setOfflineState();
  }

  renderLeds();
  delay(20);
}
