package main

import (
	"log"
	"strings"
	"sync"
	"time"
	"unicode"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const defaultMqttTopic = "wled/%s"

// wledPresetByState 是"agent 状态 -> WLED preset"的映射表。
//
// 服务端只负责把统一状态路由到固定 preset，不在服务端维护具体灯效参数。
// 具体颜色、效果、速度、亮度、矩阵布局都在 WLED 设备端的 Presets 里配置。
//
// WLED HTTP API:
//
//	T=1  打开灯
//	PL=n 调用 preset n
var wledPresetByState = map[string]string{
	"idle":     "T=1&PL=1",
	"thinking": "T=1&PL=2",
	"busy":     "T=1&PL=3",
	"approval": "T=1&PL=4",
}

// offline 状态：关灯（或灰呼吸）。这里选关灯，最省事。
const wledOfflinePayload = "T=0"

// MqttConfig 是 MQTT 推送相关的配置。
type MqttConfig struct {
	Broker   string // tcp://host:1883
	ClientID string
	User     string
	Pass     string
	Topic    string // topic 模板，见 topicFor 说明
}

// MqttPublisher 负责把 agent 状态变化推送到 WLED。
// 不启用（broker 为空）时所有方法都是 no-op，对主流程零影响。
//
// 多设备隔离：topic 和去重都按 deviceID 区分，防止多用户设备互相打架。
type MqttPublisher struct {
	cfg     MqttConfig
	enabled bool
	client  mqtt.Client

	mu       sync.Mutex
	lastSent map[string]string // deviceID -> 上次发送的 state，按设备去重
}

// NewMqttPublisher 创建发布器。broker 为空则返回一个禁用的实例。
func NewMqttPublisher(cfg MqttConfig) *MqttPublisher {
	p := &MqttPublisher{cfg: cfg, lastSent: make(map[string]string)}
	if cfg.Broker == "" {
		log.Printf("[mqtt] 未配置 broker，跳过 WLED 推送")
		return p
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "agent-light-server"
	}
	if cfg.Topic == "" {
		// 默认按设备隔离：每个 deviceID 一个独立 topic。
		cfg.Topic = defaultMqttTopic
	}
	p.cfg = cfg

	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	if cfg.User != "" {
		opts.SetUsername(cfg.User)
	}
	if cfg.Pass != "" {
		opts.SetPassword(cfg.Pass)
	}
	opts.OnConnect = func(c mqtt.Client) {
		log.Printf("[mqtt] 已连接 broker=%s", cfg.Broker)
	}
	opts.OnConnectionLost = func(c mqtt.Client, err error) {
		log.Printf("[mqtt] 连接断开: %v", err)
	}

	p.client = mqtt.NewClient(opts)
	p.enabled = true

	token := p.client.Connect()
	token.Wait()
	if token.Error() != nil {
		// 连接失败也不禁用：AutoReconnect 会持续重试。
		log.Printf("[mqtt] 初始连接失败，将在后台重试: %v", token.Error())
	}

	log.Printf("[mqtt] 已启用，broker=%s topic 模板=%s（含 %%s 则按 deviceID 展开）", cfg.Broker, cfg.Topic)
	return p
}

// topicFor 把配置里的 topic 模板解析成实际发送的 topic（带 /api 后缀）。
//
// 规则：
//   - 模板含 "%s"：按 deviceID 展开，例如 "wled/%s" + deviceID=alice -> "wled/alice/api"
//   - 模板不含 "%s"：所有设备共用一个 topic（单设备兼容老配置），例如 "wled/desk-ring/api"
//
// deviceID 会被清洗掉 MQTT topic 不允许的字符（+ # / 以及空白），用 - 替代，
// 避免用户用了奇怪的名字导致 topic 解析异常。
func (p *MqttPublisher) topicFor(deviceID string) string {
	tmpl := p.cfg.Topic
	if strings.Contains(tmpl, "%s") {
		safe := sanitizeTopicSegment(deviceID)
		return withWLEDAPISuffix(strings.ReplaceAll(tmpl, "%s", safe))
	}
	return withWLEDAPISuffix(tmpl)
}

func withWLEDAPISuffix(topic string) string {
	topic = strings.TrimRight(strings.TrimSpace(topic), "/")
	if topic == "" {
		topic = strings.ReplaceAll(defaultMqttTopic, "%s", "default")
	}
	if strings.HasSuffix(topic, "/api") {
		return topic
	}
	return topic + "/api"
}

// sanitizeTopicSegment 清洗 deviceID，去掉 MQTT topic 层级分隔符和通配符。
func sanitizeTopicSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "default"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '/' || unicode.IsSpace(r):
			return '-'
		case r == '+' || r == '#':
			return -1
		default:
			return r
		}
	}, s)
}

// PublishState 在 agent 状态变化时调用。
// 相同设备 + 相同状态不重复发送，避免每条 hook 都刷一次灯。
func (p *MqttPublisher) PublishState(deviceID, state string) {
	if p == nil || !p.enabled {
		return
	}

	payload, ok := wledPresetByState[state]
	if !ok {
		if state == "" || state == "offline" {
			payload = wledOfflinePayload
		} else {
			payload = wledPresetByState["idle"]
		}
	}

	// 按设备去重：同一设备的同一状态只发一次。
	p.mu.Lock()
	if p.lastSent[deviceID] == state {
		p.mu.Unlock()
		return
	}
	p.lastSent[deviceID] = state
	p.mu.Unlock()

	topic := p.topicFor(deviceID)
	token := p.client.Publish(topic, 0, false, payload)
	go func(t mqtt.Token, dev, st string) {
		t.Wait()
		if t.Error() != nil {
			log.Printf("[mqtt] 发送失败 device=%s state=%s: %v", dev, st, t.Error())
		} else {
			log.Printf("[mqtt] device=%s %s -> %s (topic=%s)", dev, st, payload, topic)
		}
	}(token, deviceID, state)
}

// Close 断开连接，用于优雅退出。
func (p *MqttPublisher) Close() {
	if p == nil || !p.enabled {
		return
	}
	p.client.Disconnect(200)
}
