# Phase P5 Plan: 验证、打包与回滚演练（跨平台）

**创建日期**：2026-01-13  
**状态**：🚧 Verify (2026-01-14)  
**范围**：跨平台验收、发布准备、文档与回滚演练  

---

## 目标

1. 建立跨平台验证矩阵（Windows/macOS/Linux），覆盖 MITM/Hosts/RootCA/日志/UI。
2. 对“系统改动”做回滚演练，确保用户不会因为失败/崩溃而断网或残留证书。
3. 完成发布前文档与打包资源整理。

---

## 验证矩阵（建议）

### 功能项

- MITM：启动/停止、规则命中、未命中拒绝、SSE 流式响应
- Hosts：注入/清理幂等、IPv4/IPv6、异常/权限不足提示
- Root CA：安装/卸载、重复执行、副作用检查
- 端口策略：443 或端口转发开启/关闭与状态检查
- 日志：Request Logs + Realtime Logs、复制/清空/打开目录
- UI：深色/浅色、窗口缩放、长列表性能

### 平台项（按优先级）

1. macOS（当前开发机优先）：端口策略与提权链路必须明确
2. Windows：UAC 流程与证书/hosts 的可逆性必须验证
3. Linux（至少 Debian/Ubuntu）：`update-ca-certificates` 路径优先支持

---

## 测试与工具

1. 单元测试（推荐增加）
   - hosts 文件解析/写入（marker 块增删）
   - 证书生成（Root CA + Server Cert）
   - 规则匹配（host → rule）
2. 手工验证脚本
   - 每个平台提供一份可复制的验证步骤（含失败回滚命令）
   - 仓库内置只读脚本：`scripts/verify/*`

---

## 发布准备

- 文档：
  - 用户指南：如何启用 MITM、为何需要 Root CA、风险提示
  - 故障排查：证书错误、端口占用、无法提权、清理残留
  - 回滚文档：`docs/plan/rollback/*`（至少 1 份最终版）
- 打包资源：
  - 证书导出入口与路径说明
  - 可能需要的辅助脚本/二进制（若采用 helper 模式）

---

## 验收标准

- 至少 2 个平台完成全链路验证（建议 macOS + Windows）
- 发生异常/取消提权时，系统仍可用，并能一键清理残留
- 文档与回滚流程可被非开发者按步骤执行

---

## 本轮验证记录（开发机优先：macOS）

### 0) 跨平台只读自检脚本（已补齐 2026-01-14）

- macOS：`scripts/verify/macos.sh`
- Linux：`scripts/verify/linux.sh`
- Windows：`scripts/verify/windows.ps1`

### 1) 构建与单测（已执行 2026-01-14）

- ✅ 后端：`go test ./...`
- ✅ 前端：`cd frontend && npm run build:dev`

### 2) MITM 规则转发 Smoke（纯进程内，不监听端口；建议每次改动后执行）

目的：不依赖 443/hosts/系统信任，也不需要监听端口，即可验证“规则命中 → provider 选路 → 转发请求构造（Host/Path/Auth Header）”闭环。

- 命令（使用临时 HOME，避免污染本机配置）：

  ```bash
  HOME="$(mktemp -d)" go run ./cmd/mitm-smoke
  ```

预期：返回 0 并输出 `PASS`。

已执行（2026-01-14）：`PASS`

### 3) 443 全链路手工验证（需要管理员权限）

1. 在 UI 配置并启用目标 Provider（`/` 供应商管理页）
2. 在 Rules 创建并启用规则（例如：`api.openai.com -> codex:1`）
3. 在 MITM 控制页执行：
   - 安装 Root CA
   - 应用 Hosts
   - 启动 MITM（若提示权限不足，需以管理员权限重启应用）
4. 用任意“不支持第三方 API 的 IDE”触发请求，或用 `curl` 验证（示例）：

   ```bash
   curl -k https://api.openai.com/v1/models
   ```
