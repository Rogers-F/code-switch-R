# Phase P3 Plan: 系统集成（Hosts + Root CA + 提权/端口策略）

**创建日期**：2026-01-13  
**状态**：Not Started  
**范围**：新增系统服务（跨平台），提供 UI 一键操作与自动清理策略  

---

## 目标

补齐 ghosxy 的“深度系统集成”能力，并确保可逆性与安全性：

1. Hosts 自动注入/清理：支持 IPv4/IPv6 双栈，且幂等可重复执行。
2. Root CA 一键安装/卸载：写入系统信任库（Windows/macOS/Linux）。
3. 端口策略：解决 443 绑定权限问题（按 ADR-0002 落地）。
4. 提权执行：跨平台一致的“请求权限 → 执行 → 失败回滚”链路（按 ADR-0003 落地）。

---

## 实施拆分

### Stage 1：Hosts 管理

1. 新增 `HostsService`（Go）
   - `Apply(domains, options)`：注入（IPv4/IPv6 可选）
   - `Cleanup(domains, options)`：清理（仅移除本应用的 marker 块）
   - `CheckStatus(domains)`：状态检查
2. 写入策略
   - 使用 marker 块包裹，避免误删用户自定义内容
   - 写入前备份原文件（保留最后 N 份）
   - 写入采用原子写（可复用现有 `AtomicWriteBytes` 思路）

### Stage 2：Root CA 管理

1. 新增 `SystemTrustService`（Go）
   - `Install(certPath)` / `Uninstall(certIdentity)` / `CheckInstalled()`
2. 平台策略（初版可采用命令行方式，后续可升级到更原生 API）
   - Windows：`certutil -addstore/-delstore`
   - macOS：`security add-trusted-cert` / `security delete-certificate`
   - Linux：`update-ca-certificates`（必要时兼容 `update-ca-trust`）

### Stage 3：提权执行

1. 新增 `PrivilegeService`（Go）
   - `RunElevated(command, args, opts)`：统一入口
2. 平台实现建议
   - Windows：复用 `services/updateservice.go` 的 PowerShell `Start-Process -Verb RunAs` 模式
   - macOS：`osascript`（administrator privileges）或 Authorization Services
   - Linux：`pkexec` 优先，fallback 到 `sudo`（需要在 UI 给出明确提示）

### Stage 4：端口策略（与 P2 联动）

根据 ADR-0002 执行二选一（或组合）：

1. 直接监听 443（需要管理员/特权能力；Windows 可能可行，macOS/Linux 通常需要提权）
2. 默认监听高位端口（如 8443），并提供“一键开启/关闭端口转发”
   - Windows：`netsh interface portproxy`
   - macOS：`pfctl`（需要谨慎处理规则与回滚）
   - Linux：`iptables/nftables`（需要发行版差异处理）

### Stage 5：生命周期与自动清理

1. 应用退出时 best-effort：
   - Stop MITM
   - Cleanup Hosts（仅当本应用曾注入且用户开启“自动清理”）
   - 关闭端口转发（同上）
2. 崩溃恢复：
   - 下次启动时检测“残留 marker/转发规则”，在 UI 明确提示并提供“一键清理”

---

## 验收标准

- Hosts：注入/清理幂等；不会破坏 hosts 文件其它内容；IPv4/IPv6 可选且正确
- Root CA：安装/卸载可逆；用户取消/失败时状态明确且不影响网络可用性
- 端口策略：在至少 1 个 OS 上可用，并明确其它 OS 的限制与替代方案

---

## 风险与缓解

- 不同 Linux 发行版差异：优先支持 Debian/Ubuntu（`update-ca-certificates`），其它发行版在 P5 文档中明确限制与手工命令。
- 误删 hosts 内容：只操作 marker 块；写入前备份；清理时只按 marker 删除。

---

## 参考

- ghosxy Hosts：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/main/services/HostsService.ts`
- ghosxy SystemTrust：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/main/services/SystemTrust.ts`
- code-switch-R：`services/updateservice.go`（Windows UAC 提权示例）

