#!/bin/bash
set -e

# 移除符号链接
rm -f /usr/local/bin/simonswitch 2>/dev/null || true
rm -f /usr/local/bin/codeswitch 2>/dev/null || true

# 移除自启动配置（如果存在）
AUTOSTART_NEW="${XDG_CONFIG_HOME:-$HOME/.config}/autostart/simonswitch.desktop"
AUTOSTART="${XDG_CONFIG_HOME:-$HOME/.config}/autostart/codeswitch.desktop"
rm -f "$AUTOSTART_NEW" 2>/dev/null || true
rm -f "$AUTOSTART" 2>/dev/null || true

# 更新桌面数据库
if command -v update-desktop-database &> /dev/null; then
    update-desktop-database /usr/share/applications 2>/dev/null || true
fi

echo "Simon Switch 已卸载，用户配置保留在 ~/.code-switch"
