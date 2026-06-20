# 论坛帖子：Code Switch R

---

## 帖子标题选项

**推荐标题**：
1. `[开源] Code Switch R - Claude Code/Codex/Gemini CLI 的统一管理中心，支持多供应商自动降级`
2. `分享一个工具：让 Claude Code 支持多 API 自动切换、用量统计、成本追踪`
3. `Claude Code 用户福音：一个管理多 API 供应商的桌面应用，开源免费`

---

## 帖子正文

### 标题：[开源] Code Switch R - Claude Code/Codex/Gemini CLI 的统一管理中心

---

**项目地址**：https://github.com/Rogers-F/code-switch-R

**下载地址**：https://github.com/Rogers-F/code-switch-R/releases

---

### 这是什么？

一个桌面应用，帮你**集中管理** Claude Code、Codex、Gemini CLI 的 API 供应商。

简单说就是：
- 配置多个 API 供应商（官方、第三方中转都行）
- 打开开关，CLI 请求自动走你配置的供应商
- 某个供应商挂了？自动切换到备用的
- 顺便帮你统计 Token 用量和费用

### 痛点场景

如果你也遇到过这些问题，这个工具可能对你有用：

**场景 1：API 不稳定**
> 用着用着 API 突然 429 或者超时，只能干等或者手动换配置

**场景 2：多个 API 密钥**
> 有官方的、有第三方中转的，想根据情况灵活切换，但每次都要改配置文件

**场景 3：用量不透明**
> 不知道每天用了多少 Token，月底账单吓一跳

**场景 4：MCP 配置麻烦**
> Claude Code 和 Codex 的 MCP 配置格式不一样，手动编辑 JSON/TOML 容易出错

**场景 5：WSL 配置同步**
> Windows 主机和 WSL 里都装了 Claude Code，配置要改两份

### 核心功能

#### 1. 智能供应商调度

```
请求发起
    ↓
Level 1 (最高优先级)
├── 供应商 A → 失败 (超时)
└── 供应商 B → 失败 (429)
    ↓
Level 2 (备选)
├── 供应商 C → 成功！
    ↓
返回结果
```

- 10 级优先级分组
- 失败自动降级
- 支持模型白名单和映射（比如 `claude-*` → `anthropic/claude-*`）

#### 2. 可用性监控

- 后台每分钟健康检查
- 连续失败自动拉黑（避免浪费请求）
- 分级拉黑：L1 拉黑 5 分钟，L2 拉黑 15 分钟...最高 L5 拉黑 2 小时
- 到期自动恢复

#### 3. 用量统计

- 热力图可视化
- Token 明细（输入/输出/缓存/推理）
- 成本自动计算（基于官方定价）
- 按供应商维度统计

#### 4. MCP 服务器管理

- 可视化添加/编辑
- 支持批量导入（直接粘贴 Claude Desktop 格式的 JSON）
- 一次配置，自动同步到 Claude Code 和 Codex

#### 5. WSL 一键配置（Windows）

- 自动检测已安装的 WSL 发行版
- 一键配置 WSL 中的 Claude Code/Codex/Gemini CLI
- 非破坏性合并：只更新代理相关字段，不影响你的其他配置

#### 6. 更多功能

- CLI 配置可视化编辑
- 自定义 CLI 工具支持（Droid、RooCode 等）
- Claude Skills 一键安装
- 速度测试
- 环境变量冲突检测
- 深度链接导入配置
- 多语言（中/英）
- 深色模式

### 工作原理

```
Claude Code / Codex / Gemini CLI
              ↓
      Code Switch 代理 (:18100)
              ↓
        智能供应商选择
              ↓
        实际 API 服务器
```

本地起一个代理服务，自动修改 CLI 配置指向代理。关闭代理时自动恢复原始配置。

### 下载

| 系统 | 下载 |
|------|------|
| Windows | `CodeSwitch-amd64-installer.exe` |
| macOS (M1/M2/M3/M4) | `codeswitch-macos-arm64.zip` |
| macOS (Intel) | `codeswitch-macos-amd64.zip` |
| Linux | `CodeSwitch.AppImage` |

### 使用方法

1. 下载安装
2. 添加供应商（填 API URL 和 Key）
3. 打开代理开关
4. 完成，CLI 请求自动走代理

### 截图

![主界面](https://github.com/Rogers-F/code-switch-R/raw/main/resources/images/code-switch.png)

![日志统计](https://github.com/Rogers-F/code-switch-R/raw/main/resources/images/code-switch-logs.png)

### 技术栈

- 框架：Wails 3
- 后端：Go + Gin + SQLite
- 前端：Vue 3 + TypeScript + Tailwind CSS

### 开源协议

MIT

---

**有问题欢迎反馈**：https://github.com/Rogers-F/code-switch-R/issues

---

## 可选：简短版本（适合 Twitter/即刻）

```
开源了一个 Claude Code/Codex/Gemini CLI 的管理工具 Code Switch R

✅ 多供应商自动降级
✅ 可用性监控 + 自动拉黑
✅ Token 用量统计 + 成本追踪
✅ MCP 服务器可视化管理
✅ WSL 一键配置
✅ 模型白名单和映射

GitHub: https://github.com/Rogers-F/code-switch-R
```

---

## 可选：V2EX 版本（更简洁）

```
### 分享一个 Claude Code 管理工具

**场景**：有多个 API 供应商（官方、中转），想自动切换、统计用量

**功能**：
- 多供应商自动降级
- 健康检查 + 自动拉黑
- Token/成本统计
- MCP 可视化管理
- WSL 一键配置

**原理**：本地代理，自动修改 CLI 配置

**下载**：https://github.com/Rogers-F/code-switch-R/releases

开源免费，欢迎试用反馈
```

---

## 可选：Linux.do 版本

```
## Code Switch R - Claude Code/Codex/Gemini CLI 统一管理

用 Claude Code 的朋友应该都有这个痛点：

1. API 挂了只能干等
2. 多个 Key 切换麻烦
3. 不知道用了多少钱

做了个桌面应用解决这些问题：

### 核心功能

- **多供应商降级**：配置多个 API，挂了自动切下一个
- **可用性监控**：后台检测，不可用的自动拉黑
- **用量统计**：Token 明细、成本计算、热力图
- **MCP 管理**：可视化配置，同步到 Claude Code 和 Codex
- **WSL 支持**：一键配置 WSL 中的 CLI

### 下载

Windows/macOS/Linux 都支持

https://github.com/Rogers-F/code-switch-R/releases

### 截图

[截图]

开源 MIT，欢迎反馈
```
