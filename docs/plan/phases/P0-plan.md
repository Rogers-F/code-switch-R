# Phase P0: 设计与 PoC（ADR + 可行性验证）

**创建日期**：2026-01-13  
**状态**：Planning  
**范围**：以最小改动验证可行性；不引入破坏性变更  

---

## 目标

1. 明确“前端 UI 重构 + MITM + 系统集成 + 日志优化”的总体方案与边界。
2. 产出关键 ADR，并把高风险点（端口/权限/证书安全）前置验证。
3. 完成最小 PoC：在本项目技术栈（Go + Wails3 + Vue3）里跑通“TLS SNI → 解密 → 转发 → 记录日志”的基本链路。

---

## 需要回答的关键问题（必须落到 ADR）

1. 前端：保持 Vue3 还是迁移 React（ghosxy 为 React + shadcn）？
2. MITM 监听：是否坚持 443？若无法绑定（macOS/Linux），采用 8443 + 系统端口转发是否可接受？
3. 提权：Windows/macOS/Linux 各自采用什么提权方式执行 Hosts/证书/端口转发命令？失败/取消如何回退？
4. 配置与数据：MITM 规则、证书、系统状态存储在哪里（`~/.code-switch/*` vs SQLite），如何导出/导入？

---

## PoC 范围（最小闭环）

### Backend（Go）

1. 证书 PoC
   - 生成 Root CA（本地存储）与按域名签发的 Server Cert（内存缓存即可）。
   - Go TLS 侧实现：`tls.Config.GetCertificate`（或 `GetConfigForClient`）基于 SNI 动态返回证书。
2. 代理 PoC
   - 起一个 HTTPS 监听（端口先用高位端口，避免权限问题），接收请求后转发到固定上游（或内部 mock）。
   - 支持至少 1 个流式响应用例（SSE/长连接不被截断）。
3. 日志 PoC
   - 每次请求输出结构化日志：sourceHost、matchedRule、target、status、latency、error。

### Frontend（Vue）

1. 增加一个临时页面/开关，仅用于 PoC：
   - Start/Stop MITM（后端暴露方法即可）
   - 实时日志输出（可复用现有 Console 组件）

---

## 交付物

1. ADR：`docs/plan/adr/*`
2. PoC 代码（受控在新服务/模块内，不侵入现有 Relay 栈）
3. 风险清单与缓解方案（写回 Phase 文档末尾）

---

## 验收标准

- 至少在 1 个 OS（当前开发机）上证明：
  - TLS SNI 可动态签发证书并完成握手
  - 解密后的 HTTP 请求可被转发
  - 流式响应不被破坏
  - 可观测日志可在前端实时查看
- ADR-0001/0002/0003 至少完成 Proposed → Accepted 的一次确认（需要你最终拍板）

---

## 风险与缓解（P0 聚焦）

### 已识别风险与缓解方案

1. **443 端口权限（macOS/Linux）**
   - 风险：直接监听 443 需要 root 权限，影响用户体验
   - 缓解：采用高位端口（8443）+ 端口转发策略（ADR-0002）
   - 状态：✅ 已实现 8443 默认端口

2. **Root CA 私钥安全**
   - 风险：CA 私钥泄露可能被用于中间人攻击
   - 缓解：存储在 `~/.code-switch/certs/` 目录，权限设置为 0700
   - 状态：✅ 已实现目录权限控制

3. **证书缓存内存泄漏**
   - 风险：动态生成的域名证书无限增长导致内存泄漏
   - 缓解：使用 Map 做内存缓存，当前 PoC 未设置上限
   - 状态：⚠️ P1 需要添加 LRU 缓存或定期清理

4. **Hosts 修改导致断网**
   - 风险：修改 hosts 后未清理导致无法访问目标域名
   - 缓解：使用 Marker 块标记修改内容，退出时自动清理
   - 状态：🔄 P3 实施

5. **流式响应阻塞**
   - 风险：SSE/长连接被缓冲导致延迟或中断
   - 缓解：使用 ReverseProxy 的 FlushInterval: 100ms
   - 状态：✅ 已实现

6. **TLS 握手失败**
   - 风险：客户端不信任自签名证书导致连接失败
   - 缓解：提供 CA 证书路径，用户需手动安装信任
   - 状态：✅ 已实现 GetMITMCACertPath 方法

7. **并发安全问题**
   - 风险：多线程访问证书缓存导致竞态条件
   - 缓解：使用 sync.RWMutex 保护 certCache 访问
   - 状态：✅ 已实现

8. **目标服务器证书验证跳过**
   - 风险：当前 PoC 使用 InsecureSkipVerify: true
   - 缓解：仅用于 PoC 测试，P2 实施时需要正确验证上游证书
   - 状态：⚠️ P2 修复

### P0 PoC 限制

- 固定转发目标（api.anthropic.com:443）
- 未实现基于域名的路由规则
- 未实现端口转发自动配置
- 日志仅存储在内存，无持久化
- 前端日志展示有100条限制

---

## 参考

- ghosxy：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/main/services/CertService.ts`
- ghosxy：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/main/services/ProxyService.ts`
- code-switch-R：`services/providerrelay.go`
- code-switch-R：`services/updateservice.go`（Windows 提权启动示例）

