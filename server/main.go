package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	appName          = "agent-light-server"
	defaultPort      = 4318
	defaultHost      = "127.0.0.1"
	defaultIdleTTL   = 20 * time.Minute
	defaultMaxEvents = 100
	maxBodyBytes     = 1_000_000
)

var cliFlags map[string]string

var validStates = map[string]bool{
	"idle":     true,
	"thinking": true,
	"busy":     true,
	"approval": true,
}

var lightEffects = map[string]LightEffect{
	"idle":     {Color: "green", Effect: "solid"},
	"thinking": {Color: "yellow", Effect: "breathing"},
	"busy":     {Color: "red", Effect: "solid"},
	"approval": {Color: "red", Effect: "fast_blink"},
}

type Config struct {
	Host                    string
	Port                    int
	Addr                    string
	CollectorToken          string
	DeviceToken             string
	CollectorTokenGenerated bool
	DeviceTokenGenerated    bool
	IdleTTL                 time.Duration
	MaxRecent               int
}

type Server struct {
	cfg          Config
	mu           sync.RWMutex
	devices      map[string]DeviceState
	recentEvents map[string][]RecentEvent
}

type LightEffect struct {
	Color  string
	Effect string
}

type IncomingEvent struct {
	State   string          `json:"state"`
	Source  string          `json:"source"`
	Event   *string         `json:"event"`
	Message string          `json:"message"`
	Details json.RawMessage `json:"details"`
}

type DeviceState struct {
	State       string
	Source      string
	Event       *string
	Message     string
	Details     json.RawMessage
	UpdatedAt   string
	UpdatedAtMs int64
}

type RecentEvent struct {
	State        string          `json:"state"`
	Source       string          `json:"source"`
	Event        *string         `json:"event"`
	Message      string          `json:"message"`
	Details      json.RawMessage `json:"details,omitempty"`
	ReceivedAt   string          `json:"receivedAt"`
	ReceivedAtMs int64           `json:"receivedAtMs,omitempty"`
}

type StatusResponse struct {
	State     string          `json:"state"`
	Color     string          `json:"color"`
	Effect    string          `json:"effect"`
	Message   string          `json:"message"`
	Source    *string         `json:"source"`
	Event     *string         `json:"event"`
	Details   json.RawMessage `json:"details,omitempty"`
	UpdatedAt string          `json:"updatedAt"`
}

type PostEventResponse struct {
	OK       bool           `json:"ok"`
	DeviceID string         `json:"deviceId"`
	Status   StatusResponse `json:"status"`
}

type EventsResponse struct {
	DeviceID string        `json:"deviceId"`
	Events   []RecentEvent `json:"events"`
}

func main() {
	var daemon bool
	args, flags, err := parseCLI(os.Args[1:])
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	cliFlags = flags
	if _, ok := flags["daemon"]; ok {
		daemon = true
	}

	if daemon {
		runHTTPServer()
		return
	}
	if len(args) == 0 {
		runHTTPServer()
		return
	}

	switch args[0] {
	case "help", "-h", "--help":
		printHelp()
	case "server":
		action := ""
		if len(args) > 1 {
			action = args[1]
		}
		switch action {
		case "start":
			serverStart()
		case "stop":
			serverStop()
		case "restart":
			serverRestart()
		case "status":
			serverStatus()
		default:
			fmt.Printf("用法: %s server start|stop|restart|status\n", appName)
			os.Exit(1)
		}
	default:
		fmt.Printf("未知命令: %s\n运行 '%s help' 查看帮助\n", args[0], appName)
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf(`Agent Light Server

用法:
  %[1]s                         前台启动 HTTP 服务
  %[1]s server start            后台启动 HTTP 服务
  %[1]s server stop             停止 HTTP 服务
  %[1]s server restart          重启 HTTP 服务
  %[1]s server status           查看服务状态
  %[1]s help                    显示帮助信息

默认监听:
  127.0.0.1:4318

环境变量:
  AGENT_LIGHT_PORT              监听端口，默认 4318
  AGENT_LIGHT_HOST              监听地址，默认 127.0.0.1
  AGENT_LIGHT_COLLECTOR_TOKEN   collector 上报 token；不指定则每次启动随机生成
  AGENT_LIGHT_DEVICE_TOKEN      设备查询 token；不指定则每次启动随机生成
  AGENT_LIGHT_IDLE_TTL_MS       超时回落 idle 的毫秒数
  AGENT_LIGHT_MAX_RECENT_EVENTS 每个 device 保留的最近事件数量

命令参数:
  --host <host>
  --port <port>
  --collector-token <token>
  --device-token <token>
  --idle-ttl-ms <ms>
  --max-recent-events <n>
`, appName)
}

func runHTTPServer() {
	if err := os.Chdir(appBaseDir()); err != nil {
		log.Fatalf("chdir failed: %v", err)
	}

	cfg := loadConfig()
	s := &Server{
		cfg:          cfg,
		devices:      make(map[string]DeviceState),
		recentEvents: make(map[string][]RecentEvent),
	}

	httpServer := &http.Server{
		Addr:    cfg.Addr,
		Handler: s,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Agent Light server listening on http://%s", cfg.Addr)
		log.Printf("Idle TTL: %.0fs (超时未更新 -> 绿灯)", cfg.IdleTTL.Seconds())
		log.Printf("Collector token: %s%s", cfg.CollectorToken, generatedLabel(cfg.CollectorTokenGenerated))
		log.Printf("Device token: %s%s", cfg.DeviceToken, generatedLabel(cfg.DeviceTokenGenerated))
		errCh <- httpServer.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signalNotify(sigCh)

	select {
	case sig := <-sigCh:
		log.Printf("received signal %s, shutting down", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("server shutdown failed: %v", err)
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

func loadConfig() Config {
	port := intValue("port", "AGENT_LIGHT_PORT", defaultPort)
	host := stringValue("host", "AGENT_LIGHT_HOST", defaultHost)
	collectorToken, collectorGenerated := tokenValue("collector-token", "AGENT_LIGHT_COLLECTOR_TOKEN")
	deviceToken, deviceGenerated := tokenValue("device-token", "AGENT_LIGHT_DEVICE_TOKEN")
	idleTTL := time.Duration(int64Value("idle-ttl-ms", "AGENT_LIGHT_IDLE_TTL_MS", int64(defaultIdleTTL/time.Millisecond))) * time.Millisecond
	maxRecent := intValue("max-recent-events", "AGENT_LIGHT_MAX_RECENT_EVENTS", defaultMaxEvents)
	if maxRecent < 1 {
		maxRecent = defaultMaxEvents
	}

	return Config{
		Host:                    host,
		Port:                    port,
		Addr:                    fmt.Sprintf("%s:%d", host, port),
		CollectorToken:          collectorToken,
		DeviceToken:             deviceToken,
		CollectorTokenGenerated: collectorGenerated,
		DeviceTokenGenerated:    deviceGenerated,
		IdleTTL:                 idleTTL,
		MaxRecent:               maxRecent,
	}
}

func appBaseDir() string {
	wd, _ := os.Getwd()
	if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
		return wd
	}
	exe, err := os.Executable()
	if err != nil {
		return wd
	}
	return filepath.Dir(exe)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		sendJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	deviceID, suffix, ok := parseDevicePath(r.URL.Path)
	if !ok {
		sendJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
		return
	}

	switch {
	case r.Method == http.MethodPost && suffix == "events":
		s.handlePostEvent(w, r, deviceID)
	case r.Method == http.MethodGet && suffix == "events":
		s.handleGetEvents(w, r, deviceID)
	case r.Method == http.MethodGet && suffix == "status":
		s.handleGetStatus(w, r, deviceID)
	default:
		sendJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": "not found"})
	}
}

func (s *Server) handlePostEvent(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.assertCollectorAuth(r) {
		sendJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized collector"})
		return
	}

	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}

	var event IncomingEvent
	if err := json.Unmarshal(body, &event); err != nil {
		sendJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json"})
		return
	}
	if !validStates[event.State] {
		sendJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "state must be idle, thinking, busy, or approval"})
		return
	}

	now := time.Now()
	updatedAt := formatBeijingTime(now)
	updatedAtMs := now.UnixMilli()
	source := strings.TrimSpace(event.Source)
	if source == "" {
		source = "unknown"
	}

	state := DeviceState{
		State:       event.State,
		Source:      source,
		Event:       event.Event,
		Message:     event.Message,
		Details:     objectDetails(event.Details),
		UpdatedAt:   updatedAt,
		UpdatedAtMs: updatedAtMs,
	}

	s.mu.Lock()
	s.devices[deviceID] = state
	s.rememberEventLocked(deviceID, state, updatedAt, updatedAtMs)
	status := s.resolveStatusLocked(&state, false)
	s.mu.Unlock()

	sendJSON(w, http.StatusOK, PostEventResponse{OK: true, DeviceID: deviceID, Status: status})
}

func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.assertDeviceAuth(r) {
		sendJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized device"})
		return
	}

	includeDetails := r.URL.Query().Get("details") == "1"
	s.mu.RLock()
	state, ok := s.devices[deviceID]
	var statePtr *DeviceState
	if ok {
		statePtr = &state
	}
	status := s.resolveStatusLocked(statePtr, includeDetails)
	s.mu.RUnlock()

	sendJSON(w, http.StatusOK, status)
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request, deviceID string) {
	if !s.assertDeviceAuth(r) {
		sendJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized device"})
		return
	}

	limit := queryLimit(r.URL.Query().Get("limit"), s.cfg.MaxRecent)
	includeDetails := r.URL.Query().Get("details") == "1"

	s.mu.RLock()
	events := append([]RecentEvent(nil), s.recentEvents[deviceID]...)
	s.mu.RUnlock()

	if limit < len(events) {
		events = events[:limit]
	}
	if !includeDetails {
		for i := range events {
			events[i].Details = nil
			events[i].ReceivedAtMs = 0
		}
	}

	sendJSON(w, http.StatusOK, EventsResponse{DeviceID: deviceID, Events: events})
}

func (s *Server) rememberEventLocked(deviceID string, state DeviceState, receivedAt string, receivedAtMs int64) {
	event := RecentEvent{
		State:        state.State,
		Source:       state.Source,
		Event:        state.Event,
		Message:      state.Message,
		Details:      state.Details,
		ReceivedAt:   receivedAt,
		ReceivedAtMs: receivedAtMs,
	}
	events := append([]RecentEvent{event}, s.recentEvents[deviceID]...)
	if len(events) > s.cfg.MaxRecent {
		events = events[:s.cfg.MaxRecent]
	}
	s.recentEvents[deviceID] = events
}

func (s *Server) resolveStatusLocked(deviceState *DeviceState, includeDetails bool) StatusResponse {
	now := time.Now()
	expired := deviceState == nil || deviceState.UpdatedAtMs == 0 || now.UnixMilli()-deviceState.UpdatedAtMs > s.cfg.IdleTTL.Milliseconds()
	if expired {
		var source *string
		var event *string
		message := "空闲"
		updatedAt := formatBeijingTime(now)
		if deviceState != nil {
			src := deviceState.Source
			if src == "" {
				src = "unknown"
			}
			source = &src
			event = deviceState.Event
			message = "空闲（超时未更新）"
			updatedAt = deviceState.UpdatedAt
		}

		light := lightEffects["idle"]
		return StatusResponse{
			State:     "idle",
			Color:     light.Color,
			Effect:    light.Effect,
			Message:   message,
			Source:    source,
			Event:     event,
			UpdatedAt: updatedAt,
		}
	}

	light, ok := lightEffects[deviceState.State]
	if !ok {
		light = lightEffects["idle"]
	}
	source := deviceState.Source
	if source == "" {
		source = "unknown"
	}

	status := StatusResponse{
		State:     deviceState.State,
		Color:     light.Color,
		Effect:    light.Effect,
		Message:   deviceState.Message,
		Source:    &source,
		Event:     deviceState.Event,
		UpdatedAt: deviceState.UpdatedAt,
	}
	if includeDetails {
		status.Details = nullIfEmpty(deviceState.Details)
	}
	return status
}

func parseDevicePath(path string) (string, string, bool) {
	re := regexp.MustCompile(`^/api/devices/([^/]+)/(events|status)$`)
	matches := re.FindStringSubmatch(path)
	if len(matches) != 3 {
		return "", "", false
	}
	deviceID, err := url.PathUnescape(matches[1])
	if err != nil {
		return "", "", false
	}
	return deviceID, matches[2], true
}

func sendJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.Header().Set("cache-control", "no-store")
	w.WriteHeader(statusCode)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}

func (s *Server) assertCollectorAuth(r *http.Request) bool {
	return bearerToken(r) == s.cfg.CollectorToken
}

func (s *Server) assertDeviceAuth(r *http.Request) bool {
	token := bearerToken(r)
	return token == s.cfg.DeviceToken || token == s.cfg.CollectorToken
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func objectDetails(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil
	}
	return raw
}

func nullIfEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

func formatBeijingTime(t time.Time) string {
	loc := time.FixedZone("Asia/Shanghai", 8*60*60)
	return t.In(loc).Format("2006-01-02 15:04:05")
}

func queryLimit(raw string, maxRecent int) int {
	limit := 20
	if n, err := strconv.Atoi(raw); err == nil {
		limit = n
	}
	if limit < 1 {
		return 1
	}
	if limit > maxRecent {
		return maxRecent
	}
	return limit
}

func parseCLI(args []string) ([]string, map[string]string, error) {
	flags := map[string]string{}
	remaining := make([]string, 0, len(args))
	valueFlags := map[string]bool{
		"host":              true,
		"port":              true,
		"collector-token":   true,
		"device-token":      true,
		"idle-ttl-ms":       true,
		"max-recent-events": true,
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--daemon" {
			flags["daemon"] = "1"
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			remaining = append(remaining, arg)
			continue
		}

		nameValue := strings.TrimPrefix(arg, "--")
		name := nameValue
		value := ""
		if idx := strings.Index(nameValue, "="); idx >= 0 {
			name = nameValue[:idx]
			value = nameValue[idx+1:]
		} else if valueFlags[name] {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("missing value for --%s", name)
			}
			i++
			value = args[i]
		} else {
			return nil, nil, fmt.Errorf("unknown flag: --%s", name)
		}

		if !valueFlags[name] {
			return nil, nil, fmt.Errorf("unknown flag: --%s", name)
		}
		flags[name] = value
	}

	return remaining, flags, nil
}

func stringValue(flagName, envKey, fallback string) string {
	if value := strings.TrimSpace(cliFlags[flagName]); value != "" {
		return value
	}
	return envString(envKey, fallback)
}

func intValue(flagName, envKey string, fallback int) int {
	if value := strings.TrimSpace(cliFlags[flagName]); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
		return fallback
	}
	return envInt(envKey, fallback)
}

func int64Value(flagName, envKey string, fallback int64) int64 {
	if value := strings.TrimSpace(cliFlags[flagName]); value != "" {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}
		return fallback
	}
	return envInt64(envKey, fallback)
}

func tokenValue(flagName, envKey string) (string, bool) {
	if value := strings.TrimSpace(cliFlags[flagName]); value != "" {
		return value, false
	}
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value, false
	}
	return randomToken(), true
}

func randomToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("token-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func generatedLabel(generated bool) string {
	if generated {
		return " (random for this startup)"
	}
	return ""
}

func envString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func envInt64(key string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func signalNotify(ch chan<- os.Signal) {
	signalNotifyUnix(ch, syscall.SIGINT, syscall.SIGTERM)
}
