# Ghosxy 能力迁移与前端重构 - 项目索引

**单一事实源 (Single Source of Truth)**

本文档是本次“参考 ghosxy 的能力与前端设计，对 code-switch-R 做能力补齐与 UI 重构”的唯一权威索引：阶段状态、文档链接、关键决策与回滚入口均在此追踪。

---

## 项目概览

**目标**：

1. 基于 ghosxy 的信息架构与视觉风格，完整重构本项目（code-switch-R）的前端 UI（保持 Wails3 + Vue3 技术栈）。
2. 新增 HTTPS 解密拦截（MITM）与基于域名(SNI/Host)的分流转发能力。
3. 新增系统级能力：
   - 自动化 Hosts 管理：自动注入/清理指向 127.0.0.1 的 Hosts 映射（支持 IPv4/IPv6 双栈）。
   - 根证书管理：一键安装/卸载 Root CA 到系统信任库（Windows/macOS/Linux）。
4. 参考 ghosxy 的日志系统体验，优化本项目日志体系（尤其前端展示效果）。

**参考项目**：

- ghosxy（实现参考）：`/Users/zhuoxiongliang/Documents/coding/ghosxy`
- kb-mgr（计划文档结构参考）：`/Users/zhuoxiongliang/Documents/coding/meDev/data_new/docs/kb-mgr`

**关键约束**：

- 保持现有 `:18100` 代理（Relay 模式）向后兼容；MITM 作为可选能力，不默认影响旧流程。
- 系统改动必须可逆：Hosts/证书/端口转发必须有自动清理策略与手工回滚文档。
- 最小权限原则：仅在用户显式触发时请求提权；失败/取消时保持系统处于可用状态。

---

## 实施阶段总览

| 阶段 | 名称 | 当前状态 | 负责人 | 范围冻结 | 回滚就绪 |
|------|------|---------|--------|---------|---------|
| P0 | 设计与 PoC（ADR + 可行性验证） | Completed | Claude | 是 | 否 |
| P1 | 前端 UI 2.0（对齐 ghosxy 设计） | Completed | Claude | 是 | 否 |
| P2 | HTTPS MITM 解密拦截与分流转发（核心） | Completed | Claude | 是 | 否 |
| P3 | 系统集成（Hosts + Root CA + 提权/端口策略） | Completed | Claude | 是 | 否 |
| P4 | 日志系统优化（后端 + 前端效果） | Not Started | Claude | 否 | 否 |
| P5 | 验证、打包与回滚演练（跨平台） | Not Started | Claude | 否 | 否 |

**状态图例**：

- Not Started：未开始
- Planning：计划中
- Review：审查中（关键设计/安全/兼容性评审）
- Execute：执行中
- Verify：验证中（跨平台回归）
- Completed：已完成
- Failed：失败需回滚

---

## 阶段文档链接

- Phase P0：
  - [P0-plan.md](./phases/P0-plan.md) - 设计与 PoC（ADR/可行性）
- Phase P1：
  - [P1-ui-plan.md](./phases/P1-ui-plan.md) - 前端 UI 2.0
- Phase P2：
  - [P2-mitm-plan.md](./phases/P2-mitm-plan.md) - HTTPS MITM 解密拦截与分流转发
- Phase P3：
  - [P3-system-plan.md](./phases/P3-system-plan.md) - Hosts/Root CA/提权与端口策略
- Phase P4：
  - [P4-logging-plan.md](./phases/P4-logging-plan.md) - 日志系统优化
- Phase P5：
  - [P5-verify-plan.md](./phases/P5-verify-plan.md) - 跨平台验证与发布准备

---

## 架构决策记录 (ADRs)

| ADR | 标题 | 状态 | 日期 |
|-----|------|------|------|
| [ADR-0001](./adr/ADR-0001-frontend-approach.md) | 前端重构方式（保留 Vue3 还是迁移 React） | Accepted | 2026-01-13 |
| [ADR-0002](./adr/ADR-0002-mitm-port-strategy.md) | MITM 监听端口与转发策略（443 vs 8443+端口转发） | Accepted | 2026-01-13 |
| [ADR-0003](./adr/ADR-0003-privileged-ops.md) | 跨平台提权执行策略（Hosts/证书/端口转发） | Accepted | 2026-01-13 |

---

## 回滚程序

| 范围 | 回滚文档 | 就绪状态 |
|------|---------|---------|
| 系统集成（Hosts/Root CA/端口转发） | [P0-system-rollback.md](./rollback/P0-system-rollback.md) | Draft |

---

## Gate 检查清单（通用模板）

### Planning → Review Gate

- [ ] ADR 已覆盖关键决策（前端技术路径 / MITM 端口策略 / 提权方式）
- [ ] 数据与配置落盘方案明确（存储位置、备份/恢复）
- [ ] 向后兼容策略明确（Relay 模式不受影响）
- [ ] 风险清单 ≥ 5 条且有缓解方案（安全/权限/兼容/性能/用户体验）
- [ ] 回滚文档已起草（至少覆盖 Hosts 与 Root CA）

### Review → Execute Gate

- [ ] PoC 通过（至少 1 个 OS 上完成 MITM 基础链路）
- [ ] 权限失败/取消路径验证（不会导致断网/无法访问目标域名）
- [ ] 端口策略确认（443 或 8443+端口转发）并有落地命令方案
- [ ] 测试策略明确（单元测试/手工验证清单）

### Execute → Verify Gate

- [ ] 核心功能在主机 OS 上可用（启动/停止、规则生效、日志可观测）
- [ ] Hosts 注入/清理幂等且可靠（重复执行不会破坏系统文件）
- [ ] Root CA 安装/卸载可逆（重复执行无副作用）
- [ ] 应用退出/崩溃后的“自恢复/提示”机制到位（至少有告警与一键清理）

### Verify → Completed Gate

- [ ] Windows/macOS/Linux 验证矩阵跑通（或明确标注不支持的发行版/限制）
- [ ] 文档齐全：用户指南 + 风险提示 + 回滚步骤
- [ ] 发布物包含必要资源（证书导出、辅助脚本/二进制、日志路径说明）

---

## 当前进度

**当前阶段**：P0 Completed → P1 Planning
**最近更新**：2026-01-13

**P0 完成内容**：
- ✅ 三个 ADR 已确认（前端技术栈、MITM 端口策略、提权策略）
- ✅ 后端 PoC 实现（证书生成、TLS SNI、HTTPS 代理、日志记录）
- ✅ 前端 PoC 实现（MITM 控制界面、实时日志展示）
- ✅ 风险清单与缓解方案已文档化
- ✅ 代码编译通过，路由配置完成

**下一步行动（P1 阶段）**：
1. 参考 ghosxy 的 UI 设计，建立 Vue 版基础组件库
2. 重构现有页面，对齐 ghosxy 的信息架构与视觉风格
3. 优化导航结构和交互体验

