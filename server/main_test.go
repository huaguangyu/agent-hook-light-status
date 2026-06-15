package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
	if status.State != "approval" || status.Color != "red" || status.Effect != "fast_blink" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if len(status.Details) == 0 {
		t.Fatalf("details should be included")
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

func TestLoadConfigRandomTokens(t *testing.T) {
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
}

func TestLoadConfigSpecifiedTokens(t *testing.T) {
	oldFlags := cliFlags
	cliFlags = map[string]string{
		"collector-token": "collector-from-cli",
		"device-token":    "device-from-cli",
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
}
