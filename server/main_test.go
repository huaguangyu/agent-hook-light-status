package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	testCollectorToken = "test-collector-token"
	testDeviceToken    = "test-device-token"
)

func newTestServer() *Server {
	return &Server{
		cfg: Config{
			CollectorToken: testCollectorToken,
			DeviceToken:    testDeviceToken,
			IdleTTL:        defaultIdleTTL,
			MaxRecent:      defaultMaxEvents,
		},
		devices:      make(map[string]DeviceState),
		recentEvents: make(map[string][]RecentEvent),
	}
}

func TestHealth(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestPostStatusAndEvents(t *testing.T) {
	s := newTestServer()
	body := []byte(`{"source":"manual","state":"approval","event":"ManualTest","message":"测试审批","details":{"toolName":"Bash"}}`)

	post := httptest.NewRequest(http.MethodPost, "/api/devices/desk-light-01/events", bytes.NewReader(body))
	post.Header.Set("Authorization", "Bearer "+testCollectorToken)
	post.Header.Set("Content-Type", "application/json")
	postRec := httptest.NewRecorder()
	s.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusOK {
		t.Fatalf("post status = %d, body = %s", postRec.Code, postRec.Body.String())
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/devices/desk-light-01/status?details=1", nil)
	statusReq.Header.Set("Authorization", "Bearer "+testDeviceToken)
	statusRec := httptest.NewRecorder()
	s.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("status status = %d, body = %s", statusRec.Code, statusRec.Body.String())
	}

	var status StatusResponse
	if err := json.Unmarshal(statusRec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "approval" || status.Color != "red" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if len(status.Details) == 0 {
		t.Fatalf("details should be included")
	}
	var rawStatus map[string]json.RawMessage
	if err := json.Unmarshal(statusRec.Body.Bytes(), &rawStatus); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"light", "effect", "display"} {
		if _, ok := rawStatus[key]; ok {
			t.Fatalf("status response should not include %q: %s", key, statusRec.Body.String())
		}
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/devices/desk-light-01/events?limit=20&details=1", nil)
	eventsReq.Header.Set("Authorization", "Bearer "+testDeviceToken)
	eventsRec := httptest.NewRecorder()
	s.ServeHTTP(eventsRec, eventsReq)
	if eventsRec.Code != http.StatusOK {
		t.Fatalf("events status = %d, body = %s", eventsRec.Code, eventsRec.Body.String())
	}

	var events EventsResponse
	if err := json.Unmarshal(eventsRec.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	if len(events.Events) != 1 || events.Events[0].State != "approval" {
		t.Fatalf("unexpected events: %+v", events)
	}
}

func TestIdleTTL(t *testing.T) {
	s := newTestServer()
	s.cfg.IdleTTL = time.Millisecond
	old := time.Now().Add(-time.Second)
	eventName := "ManualTest"
	s.devices["desk-light-01"] = DeviceState{
		State:       "busy",
		Source:      "manual",
		Event:       &eventName,
		Message:     "忙碌",
		UpdatedAt:   formatBeijingTime(old),
		UpdatedAtMs: old.UnixMilli(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/devices/desk-light-01/status", nil)
	req.Header.Set("Authorization", "Bearer "+testDeviceToken)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	var status StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "idle" || status.Message != "空闲（超时未更新）" {
		t.Fatalf("unexpected expired status: %+v", status)
	}
	if status.Color != "green" {
		t.Fatalf("expired status should include idle color: %+v", status)
	}
}

func TestEventsAreScopedPerDevice(t *testing.T) {
	s := newTestServer()
	s.cfg.MaxRecent = 3

	for i := 0; i < 5; i++ {
		body := []byte(`{"source":"manual","state":"thinking","event":"Alice","message":"alice"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/devices/alice-light/events", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+testCollectorToken)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("alice post status = %d, body = %s", rec.Code, rec.Body.String())
		}
	}

	body := []byte(`{"source":"manual","state":"approval","event":"Bob","message":"bob"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/devices/bob-light/events", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testCollectorToken)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bob post status = %d, body = %s", rec.Code, rec.Body.String())
	}

	aliceReq := httptest.NewRequest(http.MethodGet, "/api/devices/alice-light/events?limit=20&details=1", nil)
	aliceReq.Header.Set("Authorization", "Bearer "+testDeviceToken)
	aliceRec := httptest.NewRecorder()
	s.ServeHTTP(aliceRec, aliceReq)

	var aliceEvents EventsResponse
	if err := json.Unmarshal(aliceRec.Body.Bytes(), &aliceEvents); err != nil {
		t.Fatal(err)
	}
	if len(aliceEvents.Events) != 3 {
		t.Fatalf("alice should keep only 3 events, got %d", len(aliceEvents.Events))
	}
	for _, event := range aliceEvents.Events {
		if event.Event == nil || *event.Event != "Alice" {
			t.Fatalf("alice events mixed with another device: %+v", aliceEvents)
		}
	}

	bobReq := httptest.NewRequest(http.MethodGet, "/api/devices/bob-light/events?limit=20&details=1", nil)
	bobReq.Header.Set("Authorization", "Bearer "+testDeviceToken)
	bobRec := httptest.NewRecorder()
	s.ServeHTTP(bobRec, bobReq)

	var bobEvents EventsResponse
	if err := json.Unmarshal(bobRec.Body.Bytes(), &bobEvents); err != nil {
		t.Fatal(err)
	}
	if len(bobEvents.Events) != 1 || bobEvents.Events[0].Event == nil || *bobEvents.Events[0].Event != "Bob" {
		t.Fatalf("bob events are wrong: %+v", bobEvents)
	}
}

func TestMqttTopicForDeviceTemplate(t *testing.T) {
	p := NewMqttPublisher(MqttConfig{})
	p.cfg.Topic = "wled/%s"

	if got := p.topicFor("alice-light"); got != "wled/alice-light/api" {
		t.Fatalf("topicFor alice = %q", got)
	}
	if got := p.topicFor("team/a + #1"); got != "wled/team-a--1/api" {
		t.Fatalf("topicFor sanitized device = %q", got)
	}
}

func TestMqttTopicTemplateDoesNotDoubleAPISuffix(t *testing.T) {
	p := NewMqttPublisher(MqttConfig{})
	p.cfg.Topic = "wled/%s/api"

	if got := p.topicFor("alice-light"); got != "wled/alice-light/api" {
		t.Fatalf("topicFor with api suffix = %q", got)
	}
}

func TestMqttTopicTemplateOnlyReplacesDevicePlaceholder(t *testing.T) {
	p := NewMqttPublisher(MqttConfig{})
	p.cfg.Topic = "wled/%s/%done"

	if got := p.topicFor("alice-light"); got != "wled/alice-light/%done/api" {
		t.Fatalf("topicFor percent literal = %q", got)
	}
}

func TestMqttTopicForFixedTopic(t *testing.T) {
	p := NewMqttPublisher(MqttConfig{})
	p.cfg.Topic = "wled/desk-ring"

	if got := p.topicFor("alice-light"); got != "wled/desk-ring/api" {
		t.Fatalf("fixed topic should remain compatible, got %q", got)
	}
}

func TestLoadConfigRandomTokens(t *testing.T) {
	withTempAppBase(t)
	oldFlags := cliFlags
	cliFlags = map[string]string{}
	defer func() { cliFlags = oldFlags }()
	t.Setenv("AGENT_LIGHT_COLLECTOR_TOKEN", "")
	t.Setenv("AGENT_LIGHT_DEVICE_TOKEN", "")

	cfg := loadConfig()
	if cfg.CollectorToken == "" || cfg.DeviceToken == "" {
		t.Fatalf("tokens should be generated: %+v", cfg)
	}
	if !cfg.CollectorTokenGenerated || !cfg.DeviceTokenGenerated {
		t.Fatalf("tokens should be marked generated: %+v", cfg)
	}
	if cfg.CollectorToken == "dev-collector-token" || cfg.DeviceToken == "dev-device-token" {
		t.Fatalf("tokens should not use old fixed defaults: %+v", cfg)
	}

	var env EnvFile
	data, err := os.ReadFile(filepath.Join(appBaseDir(), envFileName))
	if err != nil {
		t.Fatalf("env.json should be created: %v", err)
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env.CollectorToken != cfg.CollectorToken || env.DeviceToken != cfg.DeviceToken {
		t.Fatalf("generated tokens should be persisted, env=%+v cfg=%+v", env, cfg)
	}
	if env.MqttTopic != defaultMqttTopic {
		t.Fatalf("env should include mqtt topic, got %+v", env)
	}
}

func TestLoadConfigSpecifiedTokens(t *testing.T) {
	withTempAppBase(t)
	oldFlags := cliFlags
	cliFlags = map[string]string{
		"collector-token": "collector-from-cli",
		"device-token":    "device-from-cli",
		"port":            "4321",
		"mqtt-broker":     "tcp://mqtt.example:1883",
		"mqtt-user":       "mqtt-user",
		"mqtt-pass":       "mqtt-pass",
	}
	defer func() { cliFlags = oldFlags }()
	_ = os.Unsetenv("AGENT_LIGHT_COLLECTOR_TOKEN")
	_ = os.Unsetenv("AGENT_LIGHT_DEVICE_TOKEN")

	cfg := loadConfig()
	if cfg.CollectorToken != "collector-from-cli" || cfg.DeviceToken != "device-from-cli" {
		t.Fatalf("cli tokens not used: %+v", cfg)
	}
	if cfg.CollectorTokenGenerated || cfg.DeviceTokenGenerated {
		t.Fatalf("specified tokens should not be marked generated: %+v", cfg)
	}
	if cfg.Port != 4321 || cfg.Mqtt.Broker != "tcp://mqtt.example:1883" {
		t.Fatalf("cli config not used: %+v", cfg)
	}

	var env EnvFile
	data, err := os.ReadFile(filepath.Join(appBaseDir(), envFileName))
	if err != nil {
		t.Fatalf("env.json should be created: %v", err)
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env.CollectorToken != "collector-from-cli" || env.DeviceToken != "device-from-cli" || env.Port != 4321 {
		t.Fatalf("cli values should be persisted: %+v", env)
	}
	if env.MqttBroker != "tcp://mqtt.example:1883" || env.MqttUser != "mqtt-user" || env.MqttPass != "mqtt-pass" {
		t.Fatalf("mqtt cli values should be persisted: %+v", env)
	}
}

func withTempAppBase(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWd)
	})
}
