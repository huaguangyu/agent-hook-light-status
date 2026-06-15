package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type RuntimeState struct {
	PID                     int    `json:"pid"`
	Addr                    string `json:"addr"`
	Host                    string `json:"host"`
	Port                    int    `json:"port"`
	CollectorToken          string `json:"collectorToken"`
	DeviceToken             string `json:"deviceToken"`
	CollectorTokenGenerated bool   `json:"collectorTokenGenerated"`
	DeviceTokenGenerated    bool   `json:"deviceTokenGenerated"`
	IdleTTLMS               int64  `json:"idleTtlMs"`
	MaxRecentEvents         int    `json:"maxRecentEvents"`
	StartedAt               string `json:"startedAt"`
}

func runtimeDir() string {
	dir := filepath.Join(appBaseDir(), "run")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func pidFile() string {
	return filepath.Join(runtimeDir(), "app.pid")
}

func stateFile() string {
	return filepath.Join(runtimeDir(), "app.json")
}

func readPID() (int, error) {
	data, err := os.ReadFile(pidFile())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func isRunning() bool {
	pid, err := readPID()
	if err != nil || pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func serverStart() {
	if isRunning() {
		pid, _ := readPID()
		fmt.Printf("服务已在运行中 (PID: %d)\n", pid)
		return
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Printf("获取可执行文件失败: %s\n", err)
		os.Exit(1)
	}

	cfg := loadConfig()
	cmd := exec.Command(exe, daemonArgs(cfg)...)
	cmd.Dir = appBaseDir()
	logFilePath := filepath.Join(runtimeDir(), "app.log")
	logFile, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		defer logFile.Close()
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		fmt.Printf("启动失败: %s\n", err)
		os.Exit(1)
	}
	_ = os.WriteFile(pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	_ = writeRuntimeState(cmd.Process.Pid, cfg)

	time.Sleep(500 * time.Millisecond)
	if cmd.Process.Signal(syscall.Signal(0)) == nil {
		fmt.Printf("服务已启动 (PID: %d)\n", cmd.Process.Pid)
		printRuntimeConfig(cfg)
		return
	}

	fmt.Println("服务启动失败")
	_ = os.Remove(pidFile())
	_ = os.Remove(stateFile())
	os.Exit(1)
}

func serverStop() {
	pid, err := readPID()
	if err != nil {
		fmt.Println("服务未在运行")
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Println("服务未在运行")
		_ = os.Remove(pidFile())
		return
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Println("服务未在运行")
		_ = os.Remove(pidFile())
		return
	}

	for i := 0; i < 20; i++ {
		time.Sleep(200 * time.Millisecond)
		if proc.Signal(syscall.Signal(0)) != nil {
			fmt.Printf("服务已停止 (PID: %d)\n", pid)
			_ = os.Remove(pidFile())
			_ = os.Remove(stateFile())
			return
		}
	}

	_ = proc.Signal(syscall.SIGKILL)
	fmt.Printf("服务已强制停止 (PID: %d)\n", pid)
	_ = os.Remove(pidFile())
	_ = os.Remove(stateFile())
}

func serverRestart() {
	if isRunning() {
		serverStop()
	}
	serverStart()
}

func serverStatus() {
	if isRunning() {
		pid, _ := readPID()
		fmt.Printf("服务运行中 (PID: %d)\n", pid)
		if state, err := readRuntimeState(); err == nil {
			fmt.Printf("Address: http://%s\n", state.Addr)
			fmt.Printf("Collector token: %s%s\n", state.CollectorToken, generatedLabel(state.CollectorTokenGenerated))
			fmt.Printf("Device token: %s%s\n", state.DeviceToken, generatedLabel(state.DeviceTokenGenerated))
			fmt.Printf("Idle TTL: %.0fs\n", float64(state.IdleTTLMS)/1000)
			fmt.Printf("Max recent events per deviceId: %d\n", state.MaxRecentEvents)
			fmt.Printf("Started at: %s\n", state.StartedAt)
		}
		return
	}
	fmt.Println("服务未运行")
	if _, err := os.Stat(pidFile()); err == nil {
		_ = os.Remove(pidFile())
	}
	if _, err := os.Stat(stateFile()); err == nil {
		_ = os.Remove(stateFile())
	}
}

func daemonArgs(cfg Config) []string {
	return []string{
		"--daemon",
		"--host", cfg.Host,
		"--port", strconv.Itoa(cfg.Port),
		"--collector-token", cfg.CollectorToken,
		"--device-token", cfg.DeviceToken,
		"--idle-ttl-ms", strconv.FormatInt(cfg.IdleTTL.Milliseconds(), 10),
		"--max-recent-events", strconv.Itoa(cfg.MaxRecent),
	}
}

func writeRuntimeState(pid int, cfg Config) error {
	state := RuntimeState{
		PID:                     pid,
		Addr:                    cfg.Addr,
		Host:                    cfg.Host,
		Port:                    cfg.Port,
		CollectorToken:          cfg.CollectorToken,
		DeviceToken:             cfg.DeviceToken,
		CollectorTokenGenerated: cfg.CollectorTokenGenerated,
		DeviceTokenGenerated:    cfg.DeviceTokenGenerated,
		IdleTTLMS:               cfg.IdleTTL.Milliseconds(),
		MaxRecentEvents:         cfg.MaxRecent,
		StartedAt:               formatBeijingTime(time.Now()),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(stateFile(), append(data, '\n'), 0600)
}

func readRuntimeState() (RuntimeState, error) {
	var state RuntimeState
	data, err := os.ReadFile(stateFile())
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func printRuntimeConfig(cfg Config) {
	fmt.Printf("Address: http://%s\n", cfg.Addr)
	fmt.Printf("Collector token: %s%s\n", cfg.CollectorToken, generatedLabel(cfg.CollectorTokenGenerated))
	fmt.Printf("Device token: %s%s\n", cfg.DeviceToken, generatedLabel(cfg.DeviceTokenGenerated))
	fmt.Printf("Max recent events per deviceId: %d\n", cfg.MaxRecent)
}
