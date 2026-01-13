#!/bin/bash
set -e

# 更新桌面数据库
if command -v update-desktop-database &> /dev/null; then
    update-desktop-database /usr/share/applications 2>/dev/null || true
fi

# 更新图标缓存
if command -v gtk-update-icon-cache &> /dev/null; then
    gtk-update-icon-cache -f -t /usr/share/icons/hicolor 2>/dev/null || true
fi

# 创建小写别名（方便命令行调用）
ln -sf /usr/local/bin/SimonSwitch /usr/local/bin/simonswitch 2>/dev/null || true
# 向后兼容旧命令名
ln -sf /usr/local/bin/SimonSwitch /usr/local/bin/codeswitch 2>/dev/null || true

echo "Simon Switch 安装完成"
