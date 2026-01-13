# Phase P1 Plan: 前端 UI 2.0（对齐 ghosxy 设计）

**创建日期**：2026-01-13
**状态**：In Progress (Stage 1 Complete, Stage 2 Complete, Stage 3 Partial)
**范围**：`frontend/`（Vue3 + Tailwind），不改后端代理核心逻辑  

---

## 目标

在不更换技术栈（仍为 Vue3）的前提下，对齐 ghosxy 的信息架构与视觉风格，完成本项目前端 UI 的整体重构，重点提升：

- 信息架构：Dashboard / Rules / Providers / Logs / Settings（其余功能收敛到 “Tools/Advanced”）
- 交互一致性：统一组件风格、间距、状态反馈、空态与错误态
- 日志体验：引入终端风格实时日志视图（与 P4 联动）

---

## 关键设计约束

- 支持浅色/深色主题（与现有 ThemeManager 兼容）
- 保持现有功能可访问（允许“新 UI 分阶段替换旧页面”，但最终需要完整迁移）
- 不手动修改 Wails 自动生成绑定文件

---

## 实施拆分

### Stage 1：信息架构与路由重组

1. 新增/调整导航结构（建议分组）：
   - Dashboard（总览 + 系统状态）
   - Providers（上游管理：Claude/Codex/Gemini/Custom）
   - Rules（新增：MITM 域名分流规则，与 P2/P3 联动）
   - Logs（请求日志 + 实时日志，P4 联动）
   - Settings（通用设置 + 网络监听 + 高级工具：MCP/Skills/Prompts/EnvCheck 等）
2. 迁移 `frontend/src/router/index.ts` 与 Sidebar 信息架构，保证可回退（可保留旧路由一段时间）。

### Stage 2：组件体系对齐 ghosxy（Vue 侧实现）

以 Tailwind 为底座，建立一组“shadcn 风格”的基础组件（Vue 实现）：

- Button / IconButton
- Card / Badge / Separator
- Table / DataGrid
- Switch / Select / Input / Textarea
- Dialog / Drawer
- ScrollArea（封装滚动容器）
- Toast/Notification（与现有 NotificationService 对齐）

### Stage 3：核心页面重构

1. Dashboard
   - 运行状态卡片：Relay、MITM、Hosts、Root CA、端口策略
   - 快捷操作：启动/停止、安装/卸载证书、应用/清理 Hosts
2. Providers
   - 列表与详情编辑体验统一（表单布局、验证、批量操作）
3. Logs
   - 保留现有“请求日志表格 + 详情抽屉”
   - 新增“终端风格实时日志”视图（P4 具体落地）
4. Settings
   - 网络监听（现有 NetworkService）与系统集成（P3）入口收敛

---

## 验收标准

- 新导航结构落地，主要功能在新 UI 下可完成闭环操作
- 主题一致性：浅色/深色模式下无明显可读性问题
- Logs 页视觉与交互明显对齐 ghosxy（终端风格视图 + 统一按钮/卡片样式）

---

## 风险与缓解

- 风险：一次性重构导致回归成本过高  
  缓解：采用“页面级迁移”，每次只替换 1 个页面并保留旧路由短期可访问；配合 P5 回归清单。

---

## 参考

- ghosxy Logs UI：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/renderer/src/pages/Logs.tsx`
- 当前 Sidebar：`frontend/src/components/Sidebar.vue`
- 当前 Logs：`frontend/src/components/Logs/Index.vue`

---

## 实施进度

### Stage 1: 信息架构与路由重组 ✅ (已完成 2026-01-13)

**完成内容：**
- 实现 5 组导航结构：Dashboard / Providers / Rules / Logs / Settings
- 新增可折叠分组功能，状态持久化到 localStorage
- 新增 `/providers` 和 `/rules` 路由与占位页面
- Settings 组整合：Console、MCP、Skills、Prompts、Env Check、Speed Test、Availability、MITM PoC
- 新增 layers 图标（Providers）和 star 图标（Rules）
- 完整国际化支持（en.json / zh.json）

**关键文件：**
- `frontend/src/components/Sidebar.vue`（新增 NavGroup 接口、分组逻辑）
- `frontend/src/components/Providers/Index.vue`（占位页面）
- `frontend/src/components/Rules/Index.vue`（占位页面）
- `frontend/src/router/index.ts`（新增路由）
- `frontend/src/locales/`（新增翻译键）

**技术亮点：**
- 分组状态通过 `collapsedGroups` ref 和 localStorage 管理
- Dashboard 组不可折叠（单项直达）
- Logs 组包含表格视图和终端视图两个子项

---

### Stage 2: 组件体系对齐 ghosxy ✅ (已完成 2026-01-13)

**完成内容：**
- 创建 5 个基础 UI 组件（Vue3 实现）
  - `Button.vue`: 支持 6 种 variant（default/destructive/outline/secondary/ghost/link）和 4 种 size
  - `Card.vue`: default/outline 两种变体
  - `Badge.vue`: default/success/warning/error/info 状态样式
  - `ScrollArea.vue`: 自定义滚动条，暴露 scrollToTop/scrollToBottom 方法
  - `Separator.vue`: horizontal/vertical 分隔线

**关键文件：**
- `frontend/src/components/ui/Button.vue`
- `frontend/src/components/ui/Card.vue`
- `frontend/src/components/ui/Badge.vue`
- `frontend/src/components/ui/ScrollArea.vue`
- `frontend/src/components/ui/Separator.vue`

**技术亮点：**
- 采用 Tailwind CSS 底座，computed 属性动态计算 class
- 组件 props 设计参考 shadcn/ui React 版本，适配 Vue3 Composition API
- 暗黑模式适配（通过 CSS 变量 `--color-*`）

---

### Stage 3: 核心页面重构 🚧 (进行中)

**已完成：**
- ✅ Logs 终端风格视图
  - 创建 `frontend/src/components/Logs/TerminalView.vue`
  - Zinc 色板深色终端（#09090b 背景、#18181b 头部）
  - 红/黄/绿终端按钮、自动滚动切换、彩色日志等级
  - 新增路由 `/logs/terminal` 和侧边栏入口

**待完成：**
- ⏳ Dashboard 页面重构（系统状态卡片）
- ⏳ Providers 页面实现（统一列表与详情编辑）
- ⏳ Settings 页面重构（网络监听与系统集成入口收敛）

**关键文件：**
- `frontend/src/components/Logs/TerminalView.vue` ✅
- `frontend/src/components/Dashboard/Index.vue` ⏳
- `frontend/src/components/Providers/Index.vue` ⏳（当前为占位）
- `frontend/src/components/General/Index.vue` (Settings) ⏳

---

## 下一步

1. 完成 Dashboard 页面重构
   - 添加运行状态卡片（Relay、MITM、Hosts、Root CA）
   - 快捷操作按钮（启动/停止、安装/卸载证书）
2. 实现 Providers 页面
   - 列表视图（所有供应商）
   - 详情编辑表单（统一布局与验证）
3. 重构 Settings 页面
   - 整合网络监听配置
   - 整合系统集成入口

