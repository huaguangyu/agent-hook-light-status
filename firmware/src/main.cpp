// Agent Light 固件 —— ESP32-C3 SuperMini
// 连 Wi-Fi → 定时 HTTP 轮询 server → 按 color/effect 点亮 GPIO0/1/2（绿/黄/红）
// 纯 HTTP 版（server 域名见 SERVER_HOST）。

#include <Arduino.h>
#include <WiFi.h>
#include <HTTPClient.h>
#include <ArduinoJson.h>

// ============ 改这里（同用户/灯的 collector 用同一个 DEVICE_ID）============
const char* WIFI_SSID     = "Mojito";
const char* WIFI_PASSWORD = "Huaguangyu";
const char* SERVER_HOST   = "your-server.example.com"; // 不带 http:// 和端口，走 80
const char* DEVICE_ID     = "desk-light-01";
const char* DEVICE_TOKEN  = "replace-with-device-token";
const int   POLL_MS       = 3000;    // 多久查一次状态
const int   OFFLINE_MS    = 30000;   // 连续这么久没拿到新状态 → 全灭（掉线指示）
const int   WIFI_TIMEOUT_MS = 30000; // 单次 Wi-Fi 连接最长等待
const int   WIFI_RETRIES    = 3;     // e-ink 项目同款：失败后彻底重置再试
// =======================================================================

// LED 接线：anode 接 GPIO，active-high（HIGH=亮）
static const uint8_t PIN_GREEN  = 0;
static const uint8_t PIN_YELLOW = 1;
static const uint8_t PIN_RED    = 2;

// server 返回的 effect 映射成驱动模式
enum class Mode : uint8_t { OFF, SOLID, SLOW_BLINK, FAST_BLINK };

Mode          mode       = Mode::OFF;
uint8_t       activePin  = 0xFF;     // 当前点亮的引脚，0xFF = 全灭

// 动画状态
bool          slowBlinkOn = false;   // 黄灯慢闪：要么全亮 要么全灭
bool          blinkOn     = false;   // 红灯快闪

// 计时（非阻塞）
unsigned long lastPoll     = 0;
unsigned long lastSuccess  = 0;
unsigned long lastSlowBlink = 0;
unsigned long lastBlink    = 0;
unsigned long lastReconnect = 0;

const char* wifiStatusName(wl_status_t status) {
  switch (status) {
    case WL_IDLE_STATUS:     return "IDLE";
    case WL_NO_SSID_AVAIL:   return "NO_SSID";
    case WL_SCAN_COMPLETED:  return "SCAN_DONE";
    case WL_CONNECTED:       return "CONNECTED";
    case WL_CONNECT_FAILED:  return "CONNECT_FAILED";
    case WL_CONNECTION_LOST: return "CONNECTION_LOST";
    case WL_DISCONNECTED:    return "DISCONNECTED";
    default:                 return "UNKNOWN";
  }
}

uint8_t colorToPin(const char* c) {
  if (strcmp(c, "green")  == 0) return PIN_GREEN;
  if (strcmp(c, "yellow") == 0) return PIN_YELLOW;
  if (strcmp(c, "red")    == 0) return PIN_RED;
  return 0xFF;
}

Mode effectToMode(const char* e) {
  if (strcmp(e, "breathing")  == 0) return Mode::SLOW_BLINK;  // 黄灯慢闪：常亮3s 灭1s
  if (strcmp(e, "fast_blink") == 0) return Mode::FAST_BLINK;
  return Mode::SOLID;  // solid 或未知都按常亮
}

// 黄灯慢闪节奏（要么全亮 要么全灭，不要半亮）
const unsigned long SLOW_BLINK_ON_MS  = 3000;   // 常亮时长 ms
const unsigned long SLOW_BLINK_OFF_MS = 1000;   // 熄灭时长 ms

void updateSlowBlink(unsigned long now) {
  unsigned long span = slowBlinkOn ? SLOW_BLINK_ON_MS : SLOW_BLINK_OFF_MS;
  if (now - lastSlowBlink >= span) {
    slowBlinkOn = !slowBlinkOn;
    lastSlowBlink = now;
  }
}

void updateBlink(unsigned long now) {
  if (now - lastBlink >= 120) {        // ~8Hz 快闪
    blinkOn = !blinkOn;
    lastBlink = now;
  }
}

// 每次循环都写一遍三个引脚：activePin 按 mode 给占空比，其余给 0。
void writeLeds() {
  uint8_t pins[3] = { PIN_GREEN, PIN_YELLOW, PIN_RED };
  uint8_t duties[3] = { 0, 0, 0 };

  if (mode != Mode::OFF && activePin != 0xFF) {
    uint8_t d = 0;
    switch (mode) {
      case Mode::SOLID:      d = 255; break;
      case Mode::SLOW_BLINK: d = slowBlinkOn ? 255 : 0; break;
      case Mode::FAST_BLINK: d = blinkOn ? 255 : 0; break;
      default: break;
    }
    for (int i = 0; i < 3; i++) if (pins[i] == activePin) duties[i] = d;
  }
  for (int i = 0; i < 3; i++) analogWrite(pins[i], duties[i]);
}

void initLedsOff() {
  uint8_t pins[3] = { PIN_GREEN, PIN_YELLOW, PIN_RED };
  for (int i = 0; i < 3; i++) {
    digitalWrite(pins[i], LOW);
    pinMode(pins[i], OUTPUT);
    analogWrite(pins[i], 0);
  }
}

void tickLedsWhileWaiting() {
  unsigned long now = millis();
  updateSlowBlink(now);
  updateBlink(now);
  writeLeds();
}

void printMatchingNetworks() {
  Serial.printf("[wifi] scanning for SSID \"%s\" ...\n", WIFI_SSID);
  int count = WiFi.scanNetworks();
  if (count <= 0) {
    Serial.println(F("[wifi] scan found no networks"));
    return;
  }

  bool found = false;
  for (int i = 0; i < count; i++) {
    if (WiFi.SSID(i) == WIFI_SSID) {
      found = true;
      Serial.printf("[wifi] found ssid=%s rssi=%d channel=%d bssid=%s\n",
                    WiFi.SSID(i).c_str(),
                    WiFi.RSSI(i),
                    WiFi.channel(i),
                    WiFi.BSSIDstr(i).c_str());
    }
  }
  if (!found) {
    Serial.printf("[wifi] SSID \"%s\" not visible in %d scanned networks\n", WIFI_SSID, count);
  }
  WiFi.scanDelete();
}

bool connectWifi(bool scanOnFailure) {
  if (WiFi.status() == WL_CONNECTED) return true;

  for (int retry = 1; retry <= WIFI_RETRIES; retry++) {
    Serial.printf("[wifi] connecting, attempt=%d/%d ssid=%s\n", retry, WIFI_RETRIES, WIFI_SSID);

    // ESP32-C3 有时会卡在旧连接状态里；失败重试前完整重置 radio 更稳。
    WiFi.disconnect(true, false);
    WiFi.mode(WIFI_OFF);
    delay(300);
    WiFi.mode(WIFI_STA);
    WiFi.setSleep(false);
    WiFi.setTxPower(WIFI_POWER_19_5dBm);
    WiFi.setHostname("agent-light");

    WiFi.begin(WIFI_SSID, WIFI_PASSWORD);

    unsigned long started = millis();
    wl_status_t lastStatus = WiFi.status();
    Serial.printf("[wifi] status=%s\n", wifiStatusName(lastStatus));

    while (WiFi.status() != WL_CONNECTED && millis() - started < (unsigned long)WIFI_TIMEOUT_MS) {
      wl_status_t status = WiFi.status();
      if (status != lastStatus) {
        lastStatus = status;
        Serial.printf("[wifi] status=%s\n", wifiStatusName(status));
      }
      tickLedsWhileWaiting();
      delay(200);
    }

    if (WiFi.status() == WL_CONNECTED) {
      Serial.printf("[wifi] connected, IP=%s RSSI=%d channel=%d BSSID=%s\n",
                    WiFi.localIP().toString().c_str(),
                    WiFi.RSSI(),
                    WiFi.channel(),
                    WiFi.BSSIDstr().c_str());
      return true;
    }

    Serial.printf("[wifi] failed, final status=%s\n", wifiStatusName(WiFi.status()));
    WiFi.disconnect(true, false);
    delay(1200);
  }

  if (scanOnFailure) printMatchingNetworks();
  return false;
}

// 拉一次状态。成功返回 true（调用方据此刷新 lastSuccess）。
bool poll() {
  WiFiClient client;
  HTTPClient http;
  String url = String("http://") + SERVER_HOST + "/api/devices/" + DEVICE_ID + "/status";

  if (!http.begin(client, url)) {
    Serial.println(F("[poll] http.begin failed"));
    return false;
  }
  http.setConnectTimeout(2000);
  http.setTimeout(3000);
  http.addHeader(F("Authorization"), String("Bearer ") + DEVICE_TOKEN);

  int code = http.GET();
  if (code != 200) {
    Serial.printf("[poll] HTTP %d\n", code);
    http.end();
    return false;
  }
  String body = http.getString();
  http.end();

  JsonDocument doc;
  if (deserializeJson(doc, body)) {
    Serial.println(F("[poll] json parse failed"));
    return false;
  }
  const char* color  = doc["color"]  | "";
  const char* effect = doc["effect"] | "";

  uint8_t pin = colorToPin(color);
  if (pin == 0xFF) {
    Serial.printf("[poll] unknown color: %s\n", color);
    return false;
  }
  Mode m = effectToMode(effect);
  // 只有 color/effect 真变了才更新灯；和上次一致就维持当前动效不动。
  if (activePin != pin || mode != m) {
    Serial.printf("[poll] %s / %s -> GPIO%d\n", color, effect, pin);
    activePin = pin;
    mode = m;
  }
  return true;
}

void ensureWifi(unsigned long now) {
  if (WiFi.status() == WL_CONNECTED) return;
  if (now - lastReconnect < 15000) return;  // 避免断网时不断阻塞重试
  lastReconnect = now;
  connectWifi(false);
}

void setup() {
  initLedsOff();

  Serial.begin(115200);
  delay(200);
  Serial.println(F("\n== Agent Light boot =="));

  // 自检：依次点亮 G/Y/R 各 400ms，确认接线
  Serial.println(F("self-test G/Y/R"));
  analogWrite(PIN_GREEN, 255);  delay(400); analogWrite(PIN_GREEN, 0);
  analogWrite(PIN_YELLOW, 255); delay(400); analogWrite(PIN_YELLOW, 0);
  analogWrite(PIN_RED, 255);    delay(400); analogWrite(PIN_RED, 0);

  WiFi.persistent(false);
  if (!connectWifi(true)) {
    Serial.println(F("[wifi] not connected yet, will retry in loop"));
  }
  lastSuccess = millis();  // 给个掉线宽限窗，避免刚开机还没拿到数据就灭
}

void loop() {
  unsigned long now = millis();

  ensureWifi(now);

  if (WiFi.status() == WL_CONNECTED && now - lastPoll >= (unsigned long)POLL_MS) {
    lastPoll = now;
    if (poll()) lastSuccess = now;
  }

  // 持续 OFFLINE_MS 没拿到新状态 → 全灭（掉线指示）
  if (now - lastSuccess > (unsigned long)OFFLINE_MS) {
    mode = Mode::OFF;
  }

  updateSlowBlink(now);
  updateBlink(now);
  writeLeds();
}
