# 系统集成回滚程序（Hosts / Root CA / 端口转发）

**版本**：Final  
**最后更新**：2026-01-14  

---

## 适用场景（何时需要回滚）

1. 应用异常退出后，目标域名仍被指向 `127.0.0.1` 导致无法访问互联网
2. Root CA 安装后出现证书告警，需要临时卸载
3. 端口转发规则残留导致 443 被占用或网络异常

---

## 1. Hosts 清理（手工兜底）

回滚原则：只删除本应用插入的 marker 块，不要修改其它自定义条目。

本应用的 marker 块为：

- Start：`# === Code-Switch MITM Start ===`
- End：`# === Code-Switch MITM End ===`

### macOS / Linux

1. 打开 hosts 文件：`/etc/hosts`
2. 查找并删除上述 marker 块（含 Start/End 两行与中间内容）
3. 保存后立即验证目标域名是否恢复解析（或直接重试访问）

如需命令行（需要管理员权限）：

```bash
sudo nano /etc/hosts
```

自动删除（更快，建议先备份）：

macOS：

```bash
sudo cp /etc/hosts "/etc/hosts.bak.$(date +%s)"
sudo sed -i '' '/# === Code-Switch MITM Start ===/,/# === Code-Switch MITM End ===/d' /etc/hosts
```

Linux（Debian/Ubuntu 等）：

```bash
sudo cp /etc/hosts "/etc/hosts.bak.$(date +%s)"
sudo sed -i '/# === Code-Switch MITM Start ===/,/# === Code-Switch MITM End ===/d' /etc/hosts
```

### Windows

hosts 文件路径通常为：

- `C:\\Windows\\System32\\drivers\\etc\\hosts`

以管理员身份打开记事本编辑并删除 marker 块。

自动删除（PowerShell，需管理员）：

```powershell
$hosts = Join-Path $env:SystemRoot "System32\drivers\etc\hosts"
$markerStart = "# === Code-Switch MITM Start ==="
$markerEnd = "# === Code-Switch MITM End ==="

Copy-Item $hosts "$hosts.bak.$(Get-Date -Format yyyyMMddHHmmss)" -Force

$inBlock = $false
$out = New-Object System.Collections.Generic.List[string]
foreach ($line in Get-Content -LiteralPath $hosts) {
  if ($line -like "*$markerStart*") { $inBlock = $true; continue }
  if ($line -like "*$markerEnd*") { $inBlock = $false; continue }
  if (-not $inBlock) { $out.Add($line) }
}
$out | Set-Content -LiteralPath $hosts -Encoding ascii
```

---

## 2. Root CA 卸载（手工兜底）

回滚原则：仅删除本应用安装的证书（通过 Common Name 或特征字符串识别）。

### Windows（管理员）

```powershell
certutil.exe -delstore "ROOT" "Code-Switch MITM CA"
```

注意：证书 Common Name 以本项目实现为准（当前为 `Code-Switch MITM CA`）。

### macOS（管理员）

```bash
sudo security delete-certificate -c "Code-Switch MITM CA" /Library/Keychains/System.keychain
```

### Linux（管理员，Debian/Ubuntu）

```bash
sudo rm -f /usr/local/share/ca-certificates/code-switch-mitm.crt
sudo update-ca-certificates --fresh
```

其它发行版：

- 如使用 `update-ca-trust`：请按发行版文档将证书从信任目录移除后刷新信任库
- 如系统信任不在 `/usr/local/share/ca-certificates`：请在 `/etc/ssl/certs` 或对应目录中定位并删除

---

## 3. 端口转发关闭（若启用过）

当前默认方案为“直接监听 443”（ADR-0002 方案 A），**不默认启用端口转发**。
若未来引入“8443 + 端口转发”（方案 B），则端口转发策略与命令因平台而异，可参考本节占位：

- Windows：`netsh interface portproxy` 相关规则删除
- macOS：`pfctl` 相关规则回滚
- Linux：`iptables/nftables` 相关规则回滚

---

## 4. 应用层清理（推荐）

如应用仍可打开，优先在 UI 里执行：

1. 停止 MITM
2. 清理 Hosts
3. 卸载 Root CA（如不再需要）
4. 关闭端口转发

---

## 5. 验证脚本（只读）

回滚完成后，可运行对应平台脚本快速检查残留：

- macOS：`scripts/verify/macos.sh`
- Linux：`scripts/verify/linux.sh`
- Windows：`scripts/verify/windows.ps1`
