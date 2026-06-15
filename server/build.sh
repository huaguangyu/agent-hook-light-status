#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

APP_NAME="agent-light-server"

build_one() {
  local target="$1"
  local out_dir="build/${target}"

  rm -rf "$out_dir"
  mkdir -p "$out_dir" dist

  case "$target" in
    darwin-arm64)
      CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
        go build -trimpath -ldflags="-s -w" -o "${out_dir}/${APP_NAME}" .
      ;;

    linux-amd64|linux-x64)
      CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        go build -trimpath -ldflags="-s -w" -o "${out_dir}/${APP_NAME}" .
      target="linux-amd64"
      ;;

    *)
      echo "未知目标: $target"
      echo "用法: ./build.sh [all|darwin-arm64|linux-amd64|linux-x64]"
      exit 1
      ;;
  esac

  tar -C "$out_dir" -czf "dist/${APP_NAME}-${target}.tar.gz" "$APP_NAME"
  echo "已生成 dist/${APP_NAME}-${target}.tar.gz"
}

target="${1:-all}"
case "$target" in
  all)
    build_one darwin-arm64
    build_one linux-amd64
    ;;
  darwin-arm64|linux-amd64|linux-x64)
    build_one "$target"
    ;;
  *)
    echo "用法: ./build.sh [all|darwin-arm64|linux-amd64|linux-x64]"
    exit 1
    ;;
esac
