#!/bin/sh

#========================================================
# v0 分支脚本强制重定向至新仓库
#========================================================

shell_url="https://raw.githubusercontent.com/railzen/nezhazero/main/script/naza.sh"

if command -v wget >/dev/null 2>&1; then
    wget -O naza.sh "$shell_url"
elif command -v curl >/dev/null 2>&1; then
    curl -o naza.sh "$shell_url"
else
    echo "错误: 未找到 wget 或 curl，请安装其中任意一个后再试"
    exit 1
fi

chmod +x naza.sh

# 携带原参数运行新脚本
exec ./naza.sh "$@"
