# Phase P2 Plan: HTTPS MITM 解密拦截与分流转发（核心）

**创建日期**：2026-01-13
**状态**：✅ Completed (2026-01-14)
**范围**：`services/`（新增 MITM 服务 + 配置/日志联动），不破坏现有 Relay 栈  

---

## 目标

实现类似 ghosxy 的 MITM 能力：通过 Root CA 动态签发域名证书，解密 HTTPS 流量，并按“域名规则”分流转发到目标 Provider。

---

## 设计原则（必须满足）

1. 默认不影响现有 `:18100` Relay 工作流。
2. 未命中规则时采取“拒绝并给出可读错误”的安全默认，而不是盲目转发。
3. 可观测性：每个请求必须可被记录（至少：host、rule、target、status、latency、error）。

---

## 实施拆分

### Stage 1：核心服务骨架

1. 新增 `MitmProxyService`（名称可调整）
   - `Start()` / `Stop()` / `Status()`
   - 监听地址与端口可配置（由 ADR-0002 决定默认策略）
2. 新增 `CertService`（Go 实现）
   - `EnsureCA()`：生成/加载 Root CA（证书 + 私钥）
   - `GetServerTLSConfig(host)`：按域名返回可用的 `tls.Certificate`（带内存缓存）

### Stage 2：SNI 与证书动态签发

1. TLS 入口：使用 `tls.Config.GetCertificate` 或 `GetConfigForClient`
2. 兼容性：对无 SNI 客户端提供 fallback 证书（例如 `localhost`）

### Stage 3：规则匹配与反向代理转发

1. 规则匹配
   - sourceHost 取值优先级：SNI > `req.Host`（去端口）
   - 规则数据结构：`sourceHost -> targetProviderId (+ 可选 model mapping / path rewrite)`
2. 转发实现
   - 首选 `httputil.ReverseProxy`（保持 streaming/SSE）
   - 认证头策略：复用现有 `determineAuthMethod()` 的逻辑，或按域名做特殊映射（参考 ghosxy 的 `AUTH_HEADER_MAP`）
   - 上游 TLS：允许忽略自签名（可控开关），并记录风险提示

### Stage 4：与现有 Provider 体系对齐

1. 复用 Provider 配置作为 MITM 的 target（避免重复维护）
2. 必要时引入新概念：
   - `MitmRule`（独立于平台 claude/codex/gemini）
   - `MitmProfile`（可选：不同一组规则/目标）

---

## 验收标准

- 能拦截至少 1 个真实域名（通过 hosts 指向本机）并完成转发
- 流式响应（SSE）正常
- 未命中规则时返回可读错误，并在日志中记录原因

---

## 风险与缓解

- HTTP/2 与长连接兼容：优先用标准库 HTTP Server + ReverseProxy，避免手写转发细节；P5 增加验证项。
- 证书生成性能：引入域名证书缓存；必要时引入磁盘缓存（重启后复用）。

---

## 参考

- ghosxy：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/main/services/ProxyService.ts`
- ghosxy：`/Users/zhuoxiongliang/Documents/coding/ghosxy/src/main/services/CertService.ts`
- code-switch-R：`services/providerrelay.go`（转发/streaming/日志采集参考）

---

## 落地情况（与当前代码对照）

### 已实现

- TLS SNI 动态证书：`tls.Config.GetCertificate` 按域名生成并缓存证书。无 SNI 时使用 `localhost` 兜底证书（握手不直接失败）。`services/mitmservice.go`
- 规则命中 → Provider 选路 → 反代转发：`MITMRuleEngine` 读取 `RuleService` 的 `sourceHost` 规则，并通过 `ProviderService` 解析 `targetProvider`（支持 `platform` / `platform:id` / `custom:{toolId}`）。`services/mitmruleengine.go`
- 未命中规则安全默认：直接拒绝并返回可读错误（而不是盲转发）。`services/mitmruleengine.go`
- 可观测日志：每次请求记录 host、rule、target、status、latency、error；前端终端日志可查看。`services/mitmservice.go`、`frontend/src/components/Logs/TerminalView.vue`

### 已知限制（不阻塞主链路）

- 上游 TLS 校验当前允许跳过（`InsecureSkipVerify: true`），对齐 ghosxy 的“可连自签名/中转”行为；后续可在 P3/P5 做成可配置并增加风险提示。`services/mitmruleengine.go`
