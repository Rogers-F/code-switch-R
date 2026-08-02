# Code Switch

> 一站式管理你的 AI 编程助手（Claude Code / Codex / Gemini CLI）

## 这是什么？

**Code Switch** 是一个桌面应用，帮你解决以下问题：

- 有多个 AI API 密钥，想灵活切换？
- API 挂了想自动切换到备用服务？
- 想统计每天用了多少 Token、花了多少钱？
- 想集中管理 MCP 服务器配置？

**一句话总结**：装上它，打开开关，Claude Code / Codex / Gemini CLI 的请求就会自动走你配置的供应商，支持自动降级、用量统计、成本追踪。

## 快速开始

### 1. 下载安装

前往 [Releases](https://github.com/Rogers-F/code-switch-R/releases) 下载对应系统的安装包：

| 系统 | 推荐下载 |
|------|---------|
| Windows | `CodeSwitch-amd64-installer.exe` |
| macOS (M1/M2/M3) | `codeswitch-macos-arm64.zip` |
| macOS (Intel) | `codeswitch-macos-amd64.zip` |
| Linux | `CodeSwitch.AppImage` |

### 2. 添加供应商

打开应用后：

1. 点击右上角 **+** 按钮
2. 填写供应商信息：
   - **名称**：随便起，比如 "官方 API"
   - **API URL**：供应商的接口地址
   - **API Key**：你的密钥
3. 点击保存

### 3. 打开代理开关

在供应商列表上方，打开 **代理开关**（蓝色表示开启）。

完成！现在你的 Claude Code / Codex / Gemini CLI 请求会自动走 Code Switch 代理。

## 功能介绍

### 供应商管理

| 功能 | 说明 |
|------|------|
| 多供应商配置 | 可以添加多个 API 供应商 |
| 拖拽排序 | 拖动卡片调整优先级 |
| 一键启用/禁用 | 每个供应商独立开关 |
| 复制供应商 | 快速复制现有配置 |

### 供应商配置逐项说明

编辑供应商时各字段的含义与典型配置：

| 字段 | 含义 | 怎么填 |
|------|------|--------|
| 名称 | 只用于显示与区分 | 随意，如"官方 API"、"某中转" |
| API URL | 供应商的接口基础地址 | 供应商文档给的地址，**不带** `/v1/messages` 等路径后缀 |
| API Key | 该供应商的密钥 | 由代理在转发时注入，CLI 本地不需要再配 Key |
| 认证方式 | 密钥放进哪个请求头 | 默认 Bearer（`Authorization: Bearer xxx`，兼容绝大多数中转）；连官方 API 选 `x-api-key`；供应商要求特殊头名时直接填自定义头名 |
| 上游协议 | 上游接口的报文格式 | 默认 auto 自动检测；上游只有 OpenAI Chat 格式接口时选 `openai_chat`，代理会自动转换请求与响应 |
| 优先级分组（Level） | 降级顺序，1 最优先、10 兜底 | 主力供应商放 Level 1，备用放 2 以后；同组内按卡片拖拽顺序尝试 |
| 支持的模型 | 模型白名单，声明该供应商能处理哪些模型 | **留空 = 支持所有模型**。填了之后，请求模型不在名单内会自动跳过该供应商（不会把请求打到不兼容端点）。支持精确名（`claude-sonnet-4-5`）与通配符（`claude-*`） |
| 模型映射 | 把 CLI 请求的模型名改写成供应商实际使用的名字 | 如 `claude-*` → `anthropic/claude-*`。映射目标必须能被上游识别；配置了白名单时目标也必须在白名单内 |
| 备用 API 地址 | 主地址失败时同一请求内按序改试 | 每行一个，最多 4 个；仅网络失败/408/421/429/5xx 这类"换地址可能救回"的错误会切换 |
| 最大并发请求数 | 同一时刻最多向该供应商转发的请求数 | 0 = 不限。满载时请求先转其它供应商，全部满载则短暂排队 |

排错提示：请求返回 404 且提示"白名单/映射不包含该模型"时，去检查上表中的
**支持的模型** 与 **模型映射** 两项——最常见的原因是白名单填了但漏了新模型名，
或映射目标写错。

### 智能降级

当你配置了多个供应商时：

```
请求发起
    ↓
尝试 Level 1 的供应商 A → 失败
    ↓
尝试 Level 1 的供应商 B → 失败
    ↓
尝试 Level 2 的供应商 C → 成功！
    ↓
返回结果
```

**优先级分组（Level）**：
- Level 1：最高优先级（首选）
- Level 2-9：备选
- Level 10：最低优先级（兜底）

### 模型映射

不同供应商可能使用不同的模型名称，比如：
- 官方 API：`claude-sonnet-4`
- OpenRouter：`anthropic/claude-sonnet-4`

配置模型映射后，Code Switch 会自动转换，你不需要改代码。

### 用量统计

- **热力图**：可视化每日使用量
- **请求统计**：请求次数、成功率
- **Token 统计**：输入/输出 Token 数量
- **成本核算**：基于官方定价计算费用

### 请求日志与抓包

- 日志页可查看每次请求的供应商、耗时、Token 与费用明细
- 独立的 **抓包** 页：打开录制开关后，转发的最终出站请求头/正文（已脱敏已知
  认证信息）按会话录制；左侧栏保留每次抓包的会话，可随时回看、删除
- 录制中或已结束的会话都可 **导出** 为 JSON 文件（弹系统保存对话框）；
  正文仍可能包含敏感提示词内容，分享前请自行确认
- 录制开关随应用重启自动停止（未正常关闭的会话会标记"已中断"），
  会话数据本身持久保留，可单个删除或一键清空

### 配置导出与导入

- 设置页支持一键导出完整配置包（供应商、MCP、提示词、技能），默认对
  API Key 脱敏，也可选择包含明文用于迁移
- 导出包可直接在本应用重新导入；同时兼容导入 cc-switch 的配置文件

### MCP 服务器管理

集中管理 Claude Code 和 Codex 的 MCP Server：
- 可视化添加/编辑/删除
- 支持 URL 和命令两种类型
- 自动同步到两个平台

### CLI 配置编辑器

可视化编辑 CLI 配置文件：
- 查看当前配置
- 修改可编辑字段（模型、插件等）
- 添加自定义配置
- 支持解锁直接编辑原始配置

### 接入 opencode（内置预设）

除三大 CLI 外，可通过"自定义 CLI 工具"接入 opencode：

1. 主页自定义工具区点击新建，在"从预设开始"里选 **opencode**——应用会自动
   探测配置文件位置（识别 `OPENCODE_CONFIG` 环境变量与 `~/.config/opencode/`
   下的 `opencode.json` / `opencode.jsonc`）并预填全部注入规则
2. 保存后为该工具添加供应商（与三大平台的供应商配置方式一致）
3. 打开该工具的代理开关：应用会往 opencode 全局配置写入一个
   `provider.code-switch-r` 供应商块（走 `@ai-sdk/anthropic`，指向本地代理），
   已有配置只补缺失字段、绝不覆盖；关闭开关自动还原
4. 在 opencode 里 `/models` 选择 code-switch-r 下的模型即可

说明：预设默认写入一个当前可用的模型条目；想在 opencode 模型列表里看到更多
模型，在该工具的配置编辑器里往 `provider.code-switch-r.models` 下按
`"模型名": {"name": "显示名"}` 追加即可（模型名经代理的白名单/映射透传，
供应商支持什么就能用什么）。带注释的 `.jsonc` 配置暂不支持自动注入，启用时
会明确报错提示；`.json` 与 `.jsonc` 并存时以界面预选与提示为准。

### 请求清理

部分中转服务（如 LiteLLM）对请求格式要求严格，会因为多余字段报错（`Extra inputs are not permitted`）。开启请求清理后，Code Switch 会在转发前自动移除不兼容的字段和请求头。

**使用方式**：在供应商编辑弹窗中开启"请求清理"开关（Gemini 走协议适配，不提供该开关）。

**黑名单模式**：只需配置要移除的内容，其余全部保留。

| 配置项 | 说明 | 内置默认值 |
|--------|------|-----------|
| 要移除的请求体字段 | 转发前从 JSON body 顶层删除 | `prompt_caching` |
| 要移除的请求头 | 转发前删除这些 header | （空） |
| anthropic-beta 要移除的值 | 从 beta header 值中剔除 | `prompt-caching-scope-2026-01-05`, `redact-thinking-2026-02-12` |

未配置时使用内置默认值，也可展开"请求清理高级配置"按供应商自定义；某一维度显式置为空数组（手改配置文件）表示该维度什么都不删。

### 跳过 TLS 证书验证

供应商编辑弹窗中可按供应商开启"跳过 TLS 证书验证"，用于自签名证书或企业代理场景。开启后该供应商的转发与健康/连通性探测都不再校验证书。**存在中间人风险，仅对信任的内网地址开启**；默认关闭。

### 其他功能

- **技能市场**：一键安装 Claude Skills
- **速度测试**：测试供应商延迟
- **自定义提示词**：管理系统提示词
- **深度链接**：通过 `ccswitch://` 链接导入配置
- **自动更新**：内置更新检查

## 工作原理

```
Claude Code / Codex / Gemini CLI
            ↓
    Code Switch 代理 (:18100)
            ↓
    ┌───────────────────┐
    │  选择供应商        │
    │  (按优先级尝试)    │
    └───────────────────┘
            ↓
      实际 API 服务器
```

**原理简述**：
1. Code Switch 在本地 18100 端口启动代理服务
2. 自动修改 Claude Code / Codex / Gemini CLI 配置，让它们的请求发到本地代理
3. 代理根据你的配置，将请求转发到对应的供应商
4. 如果供应商失败，自动尝试下一个

## 界面预览

| 亮色主题 | 暗色主题 |
|---------|---------|
| ![亮色主界面](resources/images/code-switch.png) | ![暗色主界面](resources/images/code-swtich-dark.png) |
| ![日志亮色](resources/images/code-switch-logs.png) | ![日志暗色](resources/images/code-switch-logs-dark.png) |

## 常见问题

### 打开开关后 CLI 没反应？

1. 确认代理开关已打开（蓝色状态）
2. 重启 Claude Code / Codex / Gemini CLI
3. 检查供应商配置是否正确

### 如何查看代理是否生效？

1. 在 CLI 中发起一次对话
2. 回到 Code Switch，查看"日志"页面
3. 如果有新记录，说明代理生效

### 关闭应用后 CLI 还能用吗？

不能。Code Switch 关闭后代理服务停止，CLI 请求会失败。

**解决方案**：
- 保持 Code Switch 运行
- 或者关闭代理开关（会恢复 CLI 原始配置）

### 如何备份配置？

最简单的方式：设置页 **导出配置**，得到一个可重新导入的配置包（默认脱敏
API Key，迁移到新机器可选包含明文）。

也可以直接备份配置文件，位置：
- Windows: `%USERPROFILE%\.code-switch\`
- macOS/Linux: `~/.code-switch/`

主要文件：
- `claude-code.json` - Claude Code 供应商配置
- `codex.json` - Codex 供应商配置
- `mcp.json` - MCP 服务器配置

## 安装详细说明

### Windows

**安装器方式（推荐）**：
1. 下载 `CodeSwitch-amd64-installer.exe`
2. 双击运行，按提示安装
3. 从开始菜单启动

**便携版**：
1. 下载 `CodeSwitch.exe`
2. 放到任意目录，双击运行

### macOS

1. 下载对应芯片的 zip 文件
2. 解压得到 `Code Switch.app`
3. 拖到"应用程序"文件夹
4. 首次打开如提示"无法验证开发者"，在"系统设置 → 隐私与安全性"中允许

### Linux

**AppImage（推荐）**：
```bash
chmod +x CodeSwitch.AppImage
./CodeSwitch.AppImage
```

**DEB 包（Ubuntu/Debian）**：
```bash
sudo dpkg -i codeswitch_*.deb
sudo apt-get install -f  # 如有依赖问题
```

**RPM 包（Fedora/RHEL）**：
```bash
sudo rpm -i codeswitch-*.rpm
```

## 开发者指南

### 环境准备

```bash
# 安装 Go 1.24+
# 安装 Node.js 18+

# 安装 Wails CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

### 开发运行

```bash
wails3 task dev
```

### 构建发布

```bash
# 更新构建资源
wails3 task common:update:build-assets

# 打包当前平台
wails3 task package
```

## 技术栈

| 层级 | 技术 |
|------|------|
| 框架 | [Wails 3](https://v3.wails.io) |
| 后端 | Go 1.24 + Gin + SQLite |
| 前端 | Vue 3 + TypeScript + Tailwind CSS |
| 打包 | NSIS (Windows) / nFPM (Linux) |

## 开源协议

MIT License

---

**有问题？** 欢迎在 [Issues](https://github.com/Rogers-F/code-switch-R/issues) 反馈
