# Build

本文只记录本地构建 Go 服务端。

## 本地构建

Go 构建脚本：

```text
server/build.sh
```

常用命令：

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
./build.sh all
```

输出：

```text
server/build/darwin-arm64/agent-light-server
server/build/linux-amd64/agent-light-server
server/dist/agent-light-server-darwin-arm64
server/dist/agent-light-server-linux-amd64
```

## 单平台构建

```bash
./build.sh darwin-arm64
./build.sh linux-amd64
./build.sh linux-x64
```

| 命令 | 输出 |
| --- | --- |
| `./build.sh darwin-arm64` | Apple Silicon macOS |
| `./build.sh linux-amd64` | Linux x64 |
| `./build.sh linux-x64` | `linux-amd64` 的别名 |

服务端依赖 `github.com/eclipse/paho.mqtt.golang` 推送 WLED MQTT。构建时 `CGO_ENABLED=0`，可以在 macOS 上交叉编译 Linux x64。
