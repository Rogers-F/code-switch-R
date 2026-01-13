# ADR-0003: 跨平台提权执行策略（Hosts/证书/端口转发）

**状态**：Accepted
**日期**：2026-01-13
**决策日期**：2026-01-13  

---

## 背景

以下操作通常需要管理员权限：

- 修改 hosts 文件
- 安装/卸载系统信任 Root CA
- 开启/关闭端口转发（若采用 ADR-0002 的方案 B）

本 ADR 需要确定：各平台如何“请求权限并执行命令”，以及失败/取消时如何保持系统可用与可回滚。

---

## 决策原则

1. 最小权限：仅对单次操作提权，不让应用长期以管理员权限运行。
2. 可观测与可逆：每次提权操作必须记录日志与结果；提供一键清理残留。
3. 用户可理解：明确提示为何需要权限、将修改哪些系统资源、如何撤销。

---

## 建议方案

### Windows

- PowerShell `Start-Process -Verb RunAs` 触发 UAC（参考 `services/updateservice.go`）
- 以“子进程/子命令”方式执行具体操作（hosts/证书/端口转发），主进程保持非特权

### macOS

- `osascript` 触发管理员授权（`do shell script ... with administrator privileges`）
- 或后续升级为 Authorization Services（如需要更细粒度控制）

### Linux

- 优先 `pkexec`（polkit），fallback 到 `sudo`（需在 UI 明确提示）
- 发行版差异在文档中明确（优先支持 Debian/Ubuntu）

---

## 影响

- P3 需要实现统一的 `PrivilegeService`，屏蔽平台差异
- P4/P5 需要补充“取消提权/失败”场景日志与回滚验证

