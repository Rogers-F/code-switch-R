# Simon Switch v2.8.1

## 🐛 Bug 修复

### 备份系统安全改进
- **排除密钥文件备份**：certs/ 目录现已从备份中排除
  - 不再备份 Root CA 公钥 (ca.crt) 和私钥 (ca.key)
  - 换设备后自动重新生成密钥，更安全
  - 符合密钥管理最佳实践
- **保留用户配置**：继续备份 MITM 规则映射等用户录入数据
  - MITM 规则（app.db 中的 mitm_rules 表）
  - 供应商配置
  - 应用设置

### 为什么这样改进？
- **安全性**：密钥文件不应随配置迁移，避免潜在安全风险
- **便捷性**：换设备重新生成密钥无需用户操作，自动完成
- **最佳实践**：每台设备使用独立的 CA 密钥，隔离安全域

### 升级说明
- 现有备份不受影响
- 新备份将自动排除 certs/ 目录
- 恢复配置后首次使用 MITM 功能时会自动生成新密钥

---

# Simon Switch v2.8.0

## 🎉 重大更新：完整 MITM 代理系统

本版本实现了完整的 HTTPS 中间人代理（MITM）系统，支持域名路由、证书管理、Hosts 自动配置和实时日志监控。

## 更新亮点

### 🔐 MITM 代理核心
- **HTTPS 代理服务器**：在 `:18100` 端口创建本地代理，自动拦截和转发 HTTPS 流量
- **动态证书生成**：为每个域名动态生成证书，支持透明 TLS 解密
- **根证书管理**：一键安装/卸载根 CA 证书到系统信任存储（跨平台）
- **智能路由引擎**：基于规则的请求路由，支持模型映射和路径重写

### 🎯 规则管理系统
- **完整 CRUD 界面**：创建、编辑、启用/禁用、删除路由规则
- **域名映射**：配置源域名到目标供应商的转发规则
- **模型转换**：支持跨供应商的 AI 模型名称映射
- **优先级排序**：规则按优先级匹配，灵活控制流量走向

### 🖥️ 系统集成
- **Dashboard 集成**：统一控制面板管理所有 MITM 功能
  - 启动/停止代理服务
  - 安装/卸载根证书
  - 应用/清理 Hosts 条目
  - 导出 CA 证书文件
- **Hosts 自动管理**：自动修改系统 hosts 文件，重定向域名到本地代理
- **权限提升**：跨平台管理员权限提升（UAC/sudo/pkexec）

### 📊 实时日志监控
- **MitmPoC 日志页面**：实时显示所有代理请求
- **搜索过滤**：按域名、路径、方法、错误信息快速筛选
- **性能指标**：显示每个请求的状态码和延迟时间
- **自动刷新**：可选的自动日志刷新和滚动

## 详细变更

### 🆕 新增功能

#### MITM 服务 (P2/P3)
- `MITMService`: 核心代理服务，支持启动/停止、端口配置、CA 证书管理
- `RuleService`: 规则引擎服务，完整的路由规则 CRUD 和匹配逻辑
- `HostsService`: 系统 hosts 文件管理，支持标记化安全修改和自动备份
- `SystemTrustService`: 跨平台根证书信任管理

#### UI 组件
- **Rules 页面** (`/rules`): 533 行完整规则管理界面
  - 卡片式列表展示
  - 模态对话框编辑
  - 状态徽章和操作按钮
  - 空状态引导
- **Dashboard MITM 控制**:
  - 4 个控制按钮（启停/证书/Hosts）
  - 实时系统状态指示器
  - 加载状态和错误处理
- **MitmPoC 日志增强**:
  - 搜索过滤输入框
  - 清空日志按钮
  - 日志计数显示
  - 自动滚动到最新

#### 国际化
- 新增 21+ 翻译键（中英文完整）
  - `dashboard.mitm.*`: 10 个控制相关
  - `rules.*`: 11 个规则管理相关

### 🔧 技术改进

#### 跨平台支持
- **Windows**:
  - UAC 权限提升（PowerShell Start-Process -Verb RunAs）
  - certutil.exe 证书管理
  - 系统环境变量处理
- **macOS**:
  - osascript 管理员权限
  - security 命令钥匙串管理
  - 动态库签名支持
- **Linux**:
  - pkexec/sudo 权限提升
  - update-ca-certificates
  - 多发行版兼容（Debian/Ubuntu/Fedora/Arch）

#### 安全特性
- **权限最小化**: 临时文件 + 提权复制模式修改系统文件
- **自动备份**: Hosts 修改前自动备份（保留最近 5 个）
- **标记化管理**: 使用标记块管理 hosts 条目，避免误删其他配置
- **证书隔离**: CA 证书存储在用户目录，不污染系统
- **审计日志**: 所有系统修改操作都有详细日志

#### 性能优化
- **并发安全**: 使用 sync.Mutex 保护 hosts 文件并发修改
- **内存优化**: 日志自动限制为最近 100 条
- **异步刷新**: Dashboard 状态每 30 秒异步更新
- **智能缓存**: 代理连接池复用，减少 TLS 握手开销

### 🐛 修复

#### 核心修复
- 修复 HostsService 缺少权限提升导致修改失败的问题
- 修复 TypeScript 编译错误（binding import 路径、类型断言）
- 修复中文翻译 JSON 转义错误
- 修复路由大小写不一致（MitmPoC）

#### UI 修复
- 修复 Dashboard 系统状态指示器未刷新的问题
- 修复 Rules 列表空值过滤
- 修复 MitmPoC 日志重复追加的问题

## 🔨 Wails 绑定

新增 4 个服务的完整 TypeScript 绑定：
- **mitmservice.ts**: 12 个方法（代理控制、状态查询、证书管理）
- **ruleservice.ts**: 14 个方法（规则 CRUD、启用/禁用、批量操作）
- **hostsservice.ts**: 10 个方法（Hosts 应用/清理、域名查询）
- **systemtrustservice.ts**: 6 个方法（证书安装/卸载、状态检查）

总计 42 个新方法暴露给前端调用。

## 📦 构建产物

- **macOS ARM64**: `simonswitch-macos-arm64.zip` (约 15MB)
- **macOS AMD64**: `simonswitch-macos-amd64.zip` (约 16MB)
- **Windows AMD64**: `simonswitch-windows-amd64.zip` (约 14MB)
- **Linux AMD64**: `simonswitch-linux-amd64.tar.gz` (约 18MB)

## ⚠️ 升级注意

### 首次使用 MITM 功能
1. **安装根证书**: 点击 Dashboard 的 "Install Root CA" 按钮
   - Windows: 需要 UAC 确认
   - macOS: 需要输入管理员密码
   - Linux: 需要 sudo/pkexec 认证

2. **创建路由规则**: 前往 `/rules` 页面创建至少一条规则
   - 源域名：如 `api.openai.com`
   - 目标供应商：如 `claude:1`（供应商名:优先级）

3. **应用 Hosts**: 在 Dashboard 点击 "Apply Hosts" 将规则域名重定向到本地

4. **启动代理**: 点击 "Start MITM" 启动代理服务

5. **配置客户端**: 配置 Claude Code / Codex CLI 使用代理 `http://127.0.0.1:18100`

### 卸载说明
1. 点击 "Cleanup Hosts" 清理 hosts 条目
2. 点击 "Uninstall Root CA" 移除根证书
3. 停止 MITM 代理服务

### 兼容性
- 最低系统要求：
  - macOS 11.0+ (Big Sur)
  - Windows 10+
  - Linux kernel 3.10+
- 需要管理员权限才能修改系统 hosts 和信任存储

## 🙏 致谢

感谢所有用户的反馈和建议！如有问题请在 [GitHub Issues](https://github.com/anthropics/claude-code/issues) 提交。

---

**完整变更日志**: https://github.com/your-repo/compare/v2.7.2...v2.8.0
