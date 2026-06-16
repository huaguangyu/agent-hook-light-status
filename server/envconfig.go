package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// env.json 是运行目录下的可选配置文件，用来持久化 token 和 MQTT 连接信息，
// 这样日常启动（server start / --daemon）就不用每次手动输 --collector-token / --device-token。
//
// 优先级（高 -> 低）：
//   1. 命令行 flag            （--collector-token xxx）
//   2. env.json                （本文件里写的值）
//   3. 环境变量                （AGENT_LIGHT_COLLECTOR_TOKEN）
//   4. 代码默认值 / 随机生成
//
// 注意：文件不存在时会在 loadConfig 里按最终解析结果写入一份真实配置。

const envFileName = "env.json"

// EnvFile 是 env.json 的磁盘结构。
// 空字符串不会覆盖环境变量/默认值；模板会固定写出所有字段，方便用户看到可配置项。
type EnvFile struct {
	CollectorToken string `json:"collectorToken"`
	DeviceToken    string `json:"deviceToken"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	IdleTTLMS      int64  `json:"idleTtlMs"`
	MaxRecent      int    `json:"maxRecentEvents"`

	// MQTT / WLED 配置。broker 留空表示不启用 MQTT 推送。
	MqttBroker   string `json:"mqttBroker"`   // 例如 tcp://192.168.1.10:1883
	MqttClientID string `json:"mqttClientId"` // 可选，默认 agent-light-server
	MqttUser     string `json:"mqttUser"`
	MqttPass     string `json:"mqttPass"`
	MqttTopic    string `json:"mqttTopic"` // 默认 wled/%s，会按 deviceId 拼成 wled/<deviceId>/api
}

// envFilePath 返回运行目录下的 env.json 绝对路径。
func envFilePath() string {
	return filepath.Join(appBaseDir(), envFileName)
}

// loadEnvFile 读取 env.json。
// 读不到（或 JSON 损坏）时返回零值，绝不 panic，也不阻断启动。
func loadEnvFile() EnvFile {
	path := envFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return EnvFile{}
	}

	var env EnvFile
	if err := json.Unmarshal(data, &env); err != nil {
		log.Printf("[env] %s 解析失败，按空配置处理: %v", path, err)
		return EnvFile{}
	}
	return env
}

// persistEnvFile 把最终运行配置写回 env.json。
// 这样首次启动生成的随机 token、命令行传入的 token/MQTT/端口等参数都会持久化。
func persistEnvFile(cfg Config) {
	env := EnvFile{
		CollectorToken: cfg.CollectorToken,
		DeviceToken:    cfg.DeviceToken,
		Host:           cfg.Host,
		Port:           cfg.Port,
		IdleTTLMS:      cfg.IdleTTL.Milliseconds(),
		MaxRecent:      cfg.MaxRecent,
		MqttBroker:     cfg.Mqtt.Broker,
		MqttClientID:   cfg.Mqtt.ClientID,
		MqttUser:       cfg.Mqtt.User,
		MqttPass:       cfg.Mqtt.Pass,
		MqttTopic:      cfg.Mqtt.Topic,
	}
	if env.MqttClientID == "" {
		env.MqttClientID = "agent-light-server"
	}
	if env.MqttTopic == "" {
		env.MqttTopic = defaultMqttTopic
	}
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		log.Printf("[env] 生成配置失败: %v", err)
		return
	}
	data = append(data, '\n')
	path := envFilePath()
	if err := os.WriteFile(path, data, 0600); err != nil {
		log.Printf("[env] 写入配置失败 (%s): %v", path, err)
		return
	}
}

// resolveString 按优先级返回一个字符串配置：flag > env.json > 环境变量 > fallback。
func resolveString(flagName, envKey string, envValue, fallback string) string {
	if v := cliFlags[flagName]; v != "" {
		return v
	}
	if envValue != "" {
		return envValue
	}
	return fallback
}

func resolveInt(flagName, envKey string, envValue, fallback int) int {
	if v := strings.TrimSpace(cliFlags[flagName]); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
		return fallback
	}
	if envValue > 0 {
		return envValue
	}
	return envInt(envKey, fallback)
}

func resolveInt64(flagName, envKey string, envValue, fallback int64) int64 {
	if v := strings.TrimSpace(cliFlags[flagName]); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
		return fallback
	}
	if envValue > 0 {
		return envValue
	}
	return envInt64(envKey, fallback)
}
