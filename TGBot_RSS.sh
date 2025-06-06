#!/bin/bash

API_URL="https://ghproxy.badking.pp.ua/https://api.github.com/repos/IonRh/TGBot_RSS/releases/latest"

# 获取最新版本号
if [ -z "$1" ]; then
  VERSION=$(curl -s "$API_URL" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  if [ -z "$VERSION" ]; then
    echo "无法获取最新版本号"
    exit 1
  fi
else
  VERSION="$1"
fi

# 检测系统和架构
OS=$(uname | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  linux)
    case "$ARCH" in
      x86_64)   PKG="TGBot-linux-amd64.tar.gz" ;;
      aarch64)  PKG="TGBot-linux-arm64.tar.gz" ;;
      armv7l)   PKG="TGBot-linux-armv7.tar.gz" ;;
      *)        echo "不支持的 Linux 架构: $ARCH"; exit 1 ;;
    esac
    ;;
  *)
    echo "不支持的操作系统: $OS"
    exit 1
    ;;
esac

REPO_URL="https://ghproxy.badking.pp.ua/https://github.com/IonRh/TGBot_RSS/releases/download"
URL="$REPO_URL/$VERSION/$PKG"

echo "下载: $URL"
curl -L -o "$PKG" "$URL"
if [ $? -ne 0 ]; then
  echo "下载失败"
  exit 1
fi

echo "解压: $PKG"
if [ -f config.json ]; then
    echo "config.json 已存在，更新 TGBot"
    tar -xzvf "$PKG" TGBot --overwrite
else
    tar -xzvf "$PKG" --overwrite
fi
chmod +x TGBot_RSS
echo "删除压缩包: $PKG"
rm -f "$PKG"
echo "完成，请修改 TGBot_RSS 的配置文件: config.json"
echo "之后再次运行: ./TGBot_RSS"
echo "后台运行可输入：nohup ./TGBot_RSS > /dev/null 2>&1 &"
rm -f "TGBot_RSS.sh"
