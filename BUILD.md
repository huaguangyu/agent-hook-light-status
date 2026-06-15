# Build & Release

本文记录 Agent Light 的本地构建和 GitHub Actions 自动发布方式。

## 本地构建 Go 服务端

Go 服务端在 `server/` 目录，构建脚本是：

```bash
cd /Users/apple/user/VscodeProject/agent_light/server
./build.sh all
```

单独构建：

```bash
./build.sh darwin-arm64
./build.sh linux-amd64
./build.sh linux-x64
```

输出：

```text
server/build/darwin-arm64/agent-light-server
server/build/linux-amd64/agent-light-server
server/dist/agent-light-server-darwin-arm64
server/dist/agent-light-server-linux-amd64
```

本项目服务端只用 Go 标准库，`CGO_ENABLED=0`，所以 GitHub Actions 可以直接交叉编译 macOS arm64 和 Linux x64。

## CI/CD 自动构建

项目使用 GitHub Actions：

```text
.github/workflows/release.yml
```

触发规则：

| 触发 | 行为 |
| --- | --- |
| push 到 `main` | 运行 Go 测试，并构建 linux-amd64 / darwin-arm64 artifact |
| pull request 到 `main` | 运行 Go 测试，并构建 artifact |
| push `v*` tag | 构建 artifact，并自动创建 GitHub Release |

自动构建产物：

```text
agent-light-server-darwin-arm64
agent-light-server-linux-amd64
```

## 发布 Release

参考 `e-ink` 项目的发布方式，推送 `v*` tag 即可触发自动发布：

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions 完成后，带平台后缀的二进制文件会自动附加到 GitHub Release。

如果需要重新发布同一个版本，先删除本地和远程 tag，再重新打 tag：

```bash
git tag -d v0.1.0
git push origin :refs/tags/v0.1.0
git tag v0.1.0
git push origin v0.1.0
```
