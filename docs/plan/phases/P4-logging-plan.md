# Phase P4 Plan: 日志系统优化（后端 + 前端效果）

**创建日期**：2026-01-13  
**状态**：Not Started  
**范围**：日志采集、实时推送与前端展示整体优化（参考 ghosxy）  

---

## 目标

1. 前端日志体验对齐 ghosxy：提供终端风格实时日志视图（复制/清空/自动滚动/打开日志目录）。
2. 后端日志与请求日志联动：MITM/Relay/系统操作都能被追踪，便于排障与验收。
3. 性能可控：大量日志不拖慢 UI（必要时虚拟列表/限制缓冲区大小）。

---

## 实施拆分

### Stage 1：日志数据源梳理与统一出口

1. 请求日志（SQLite）：
   - 现状：`services/logservice.go` + `request_log` 表
   - 目标：补充 MITM 字段（mode/sourceHost/ruleId 等）或新增表（按 P0 决策）
2. 运行时日志（Console）：
   - 现状：`services/consoleservice.go` 与前端 Console 页
   - 目标：为 MITM/Hosts/RootCA 提供结构化事件并可实时推送

### Stage 2：后端实时推送与文件路径能力

1. 推送方式：Wails events（或复用现有 NotificationService 的事件通道）
2. 提供能力：
   - `GetLogHistory(limit)`
   - `ClearLogs()`
   - `OpenLogFolder()`（跨平台打开文件夹）

### Stage 3：前端终端风格日志组件

1. 新建 `TerminalLogView`（Vue）
   - monospace、按 level 着色、支持搜索/过滤
   - autoScroll 开关（考虑“最新在顶部/底部”的交互一致性）
2. 集成到 Logs 页
   - Logs 页拆为 Tabs：Request Logs（现有表格）/ Realtime Logs（终端视图）
   - 保留现有 RequestDetailDrawer 能力

---

## 验收标准

- Realtime Logs 具备：复制、清空、自动滚动、打开日志目录（如平台允许）
- 大量日志（例如 2000 行）渲染不卡顿（可接受的交互延迟）
- 请求日志能区分来源（Relay vs MITM）并可过滤

---

## 风险与缓解

- 日志量过大导致内存上涨：前端与后端都做 ring buffer（限制条数），并在 UI 提示“已截断”。

---

## 参考

- ghosxy Logs UI：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/renderer/src/pages/Logs.tsx`
- ghosxy LogService：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/main/services/LogService.ts`
- 当前 Logs：`frontend/src/components/Logs/Index.vue`
- 当前 Console：`frontend/src/components/Console/Index.vue`

