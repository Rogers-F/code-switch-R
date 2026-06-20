# Gemini 供应商功能对齐方案文档

> 版本: v1.0
> 日期: 2025-12-30
> 状态: 待实施
> 审核: Codex

---

## 1. 背景与目标

### 1.1 问题概述

经过与 Codex 和 Claude Code 的多轮深度辩论分析，发现 Gemini 供应商功能与 Claude/Codex 存在以下差异：

| 功能                           | Claude/Codex | Gemini   | 状态    |
|--------------------------------|-------------|----------|---------|
| 代理转发（分级降级/轮询/拉黑） | ✅          | ✅       | ✅ 一致 |
| 模型白名单/映射                | ✅          | ❌       | ⚠️ 缺失 |
| 配置验证                       | ✅          | ❌       | ⚠️ 缺失 |
| 客户端中断不计失败             | ✅          | ❌       | ⚠️ 缺失 |
| 切换通知                       | ✅          | ❌       | ⚠️ 缺失 |
| 日志统计粒度                   | 每次尝试    | 整次请求 | ⚠️ 差异 |
| Enabled 语义                   | 仅 relay    | 双重含义 | ⚠️ 冲突 |

### 1.2 目标

使 Gemini 供应商功能与 Claude/Codex 完全一致，确保三平台行为统一。

---

## 2. 修复方案详细设计

### 2.1 P0: Enabled 语义冲突修复 (最高优先级)

#### 2.1.1 问题分析

**当前问题代码位置**: `services/geminiservice.go:320-325`

```go
// 更新启用状态
for i := range s.providers {
    s.providers[i].Enabled = (s.providers[i].ID == id)
}
return s.saveProviders()
```

**问题描述**:
- `Enabled` 同时承担"参与 relay 降级"与"直连已应用"两种含义
- 当用户切换直连时，会把其他 provider 的 `Enabled` 设为 `false`
- 破坏了托管模式下多 provider 降级配置

**Claude/Codex 的设计** (参考):
- `Provider.Enabled` **仅用于 relay 降级调度**
- 直连状态通过 `GetDirectAppliedProviderID()` 动态读取 CLI 配置判断
- 直连应用时 **不修改** 其他 provider 的 `Enabled` 状态

#### 2.1.2 修复方案

**Step 1: 修改 SwitchProvider 方法**

文件: `services/geminiservice.go:320-325`

```diff
- // 更新启用状态
- for i := range s.providers {
-     s.providers[i].Enabled = (s.providers[i].ID == id)
- }
- return s.saveProviders()
+ // Enabled 仅表示是否参与 relay 降级，直连切换不修改
+ // 直连状态通过 GetDirectAppliedProviderID() 从 CLI 配置反推
+ return nil
```

**Step 2: 修改 GetDirectAppliedProviderID 方法** (OAuth 歧义修复)

文件: `services/geminiservice.go:1007`

在方法开头增加 OAuth 唯一性检查：

```go
func (s *GeminiService) GetDirectAppliedProviderID() (*string, error) {
    // 1. 检查代理状态
    proxyStatus, err := s.ProxyStatus()
    if err != nil {
        return nil, fmt.Errorf("检查代理状态失败: %w", err)
    }
    if proxyStatus != nil && proxyStatus.Enabled {
        return nil, nil // 代理模式，无直连 provider
    }

    // 【新增】2. OAuth 模式特殊处理：只允许唯一 OAuth provider
    settings, settingsErr := readGeminiSettings()
    if settingsErr == nil {
        if security, ok := settings["security"].(map[string]any); ok {
            if auth, ok := security["auth"].(map[string]any); ok {
                if selectedType, ok := auth["selectedType"].(string); ok && selectedType == string(GeminiAuthOAuth) {
                    // OAuth 模式：查找所有 OAuth provider
                    s.mu.Lock()
                    var oauthProviders []GeminiProvider
                    for _, p := range s.providers {
                        if detectGeminiAuthType(&p) == GeminiAuthOAuth {
                            oauthProviders = append(oauthProviders, p)
                        }
                    }
                    s.mu.Unlock()

                    switch len(oauthProviders) {
                    case 1:
                        id := oauthProviders[0].ID
                        return &id, nil
                    case 0:
                        return nil, nil
                    default:
                        return nil, fmt.Errorf("存在多个 OAuth 供应商 (%d 个)，无法确定当前直连应用项", len(oauthProviders))
                    }
                }
            }
        }
    }

    // 3. 非 OAuth 仍走 URL+Key 反推（现有逻辑保持不变）
    // ...
}
```

#### 2.1.3 验收标准

- [ ] 切换直连 provider 时，其他 provider 的 `Enabled` 状态保持不变
- [ ] 托管模式下多个 `Enabled=true` 的 provider 可正常参与降级
- [ ] OAuth 模式下存在多个 OAuth provider 时返回明确错误

---

### 2.2 P1: 模型白名单/映射功能

#### 2.2.1 问题分析

**当前问题**: `GeminiProvider` 结构体缺少 `SupportedModels` 和 `ModelMapping` 字段

**参考实现**: `services/providerservice.go:37-43`

```go
// Provider 结构体（Claude/Codex）
SupportedModels map[string]bool   `json:"supportedModels,omitempty"`
ModelMapping    map[string]string `json:"modelMapping,omitempty"`
```

#### 2.2.2 修复方案

**Step 1: 扩展 GeminiProvider 结构体**

文件: `services/geminiservice.go:24-39`

```diff
  type GeminiProvider struct {
      ID                  string            `json:"id"`
      Name                string            `json:"name"`
      // ... 其他字段 ...
      SettingsConfig      map[string]any    `json:"settingsConfig,omitempty"`
+
+     // 模型白名单 - Provider 原生支持的模型名
+     // 使用 map 实现 O(1) 查找，向后兼容（omitempty）
+     SupportedModels map[string]bool `json:"supportedModels,omitempty"`
+
+     // 模型映射 - 外部模型名 -> Provider 内部模型名
+     // 支持精确匹配和通配符（如 "gemini-*" -> "models/gemini-*"）
+     ModelMapping map[string]string `json:"modelMapping,omitempty"`
  }
```

**Step 2: 添加方法**

文件: `services/geminiservice.go` (新增在结构体定义后)

```go
// IsModelSupported 检查 provider 是否支持指定的模型
// 复用 providerservice.go 中的 matchWildcard 函数（同包内可见）
func (p *GeminiProvider) IsModelSupported(modelName string) bool {
    // 向后兼容：如果未配置白名单和映射，假设支持所有模型
    if (p.SupportedModels == nil || len(p.SupportedModels) == 0) &&
        (p.ModelMapping == nil || len(p.ModelMapping) == 0) {
        return true
    }

    // 场景 A：Provider 原生支持该模型（精确匹配）
    if p.SupportedModels != nil && p.SupportedModels[modelName] {
        return true
    }

    // 场景 A+：Provider 原生支持该模型（通配符匹配）
    if p.SupportedModels != nil {
        for supportedModel := range p.SupportedModels {
            if matchWildcard(supportedModel, modelName) {
                return true
            }
        }
    }

    // 场景 B：Provider 通过映射支持该模型
    if p.ModelMapping != nil {
        if _, exists := p.ModelMapping[modelName]; exists {
            return true
        }
        for pattern := range p.ModelMapping {
            if matchWildcard(pattern, modelName) {
                return true
            }
        }
    }

    return false
}

// GetEffectiveModel 获取实际应该使用的模型名
func (p *GeminiProvider) GetEffectiveModel(requestedModel string) string {
    if p.ModelMapping == nil || len(p.ModelMapping) == 0 {
        return requestedModel
    }

    // 优先查找精确映射
    if mappedModel, exists := p.ModelMapping[requestedModel]; exists {
        return mappedModel
    }

    // 查找通配符映射
    for pattern, replacement := range p.ModelMapping {
        if matchWildcard(pattern, requestedModel) {
            return applyWildcardMapping(pattern, replacement, requestedModel)
        }
    }

    return requestedModel
}

// ValidateConfiguration 验证 provider 的模型配置
func (p *GeminiProvider) ValidateConfiguration() []string {
    errors := make([]string, 0)

    if len(p.ModelMapping) > 0 && len(p.SupportedModels) > 0 {
        for externalModel, internalModel := range p.ModelMapping {
            if strings.Contains(internalModel, "*") {
                continue // 通配符映射暂不验证
            }

            supported := false
            if p.SupportedModels[internalModel] {
                supported = true
            } else {
                for supportedPattern := range p.SupportedModels {
                    if matchWildcard(supportedPattern, internalModel) {
                        supported = true
                        break
                    }
                }
            }

            if !supported {
                errors = append(errors, fmt.Sprintf(
                    "模型映射无效：'%s' -> '%s'，目标模型 '%s' 不在 supportedModels 中",
                    externalModel, internalModel, internalModel,
                ))
            }
        }
    }

    return errors
}
```

**Step 3: 新增 URL 模型替换函数**

文件: `services/providerrelay.go` (新增在 `extractGeminiModelFromEndpoint` 函数附近)

```go
// replaceGeminiModelInEndpoint 安全地替换 Gemini endpoint 中的模型名
// 输入：endpoint = "/v1beta/models/gemini-2.5-pro:generateContent?alt=sse"
//      oldModel = "gemini-2.5-pro"
//      newModel = "gemini-2.0-flash"
// 输出："/v1beta/models/gemini-2.0-flash:generateContent?alt=sse"
func replaceGeminiModelInEndpoint(endpoint, oldModel, newModel string) (string, error) {
    if endpoint == "" || oldModel == "" {
        return endpoint, nil
    }

    // 1. 分离查询参数
    queryIdx := strings.Index(endpoint, "?")
    path := endpoint
    query := ""
    if queryIdx >= 0 {
        path = endpoint[:queryIdx]
        query = endpoint[queryIdx:]
    }

    // 2. 定位 models/ 段
    modelsIdx := strings.Index(path, "models/")
    if modelsIdx == -1 {
        return "", fmt.Errorf("endpoint 格式错误：未找到 models/ 段: %s", endpoint)
    }

    // 3. 提取 models/ 后的内容
    afterModels := path[modelsIdx+len("models/"):]

    // 4. 验证 afterModels 以 oldModel 开头
    if !strings.HasPrefix(afterModels, oldModel) {
        return "", fmt.Errorf("endpoint 中的模型名与预期不符: 期望 %s, 实际前缀 %s", oldModel, afterModels)
    }

    // 5. 提取模型名后的动作部分（如 :generateContent）
    actionPart := afterModels[len(oldModel):]

    // 6. 验证动作部分格式（必须以 : 开头或为空）
    if actionPart != "" && !strings.HasPrefix(actionPart, ":") && !strings.HasPrefix(actionPart, "/") {
        return "", fmt.Errorf("endpoint 模型名后缀格式错误: %s", actionPart)
    }

    // 7. 对新模型名进行 URL 编码（防御性编程）
    encodedNewModel := url.PathEscape(newModel)

    // 8. 重组 endpoint
    newPath := path[:modelsIdx+len("models/")] + encodedNewModel + actionPart
    return newPath + query, nil
}
```

**Step 4: 修改 geminiProxyHandler 增加模型过滤**

文件: `services/providerrelay.go` (在 activeProviders 构建处，约 1199 行)

```diff
  // 1. 过滤可用的 providers
  var activeProviders []GeminiProvider
+ requestedModel := extractGeminiModelFromEndpoint(endpoint)
  for _, p := range providers {
      if !p.Enabled || p.BaseURL == "" {
          continue
      }

+     // 【新增】配置验证
+     if errs := p.ValidateConfiguration(); len(errs) > 0 {
+         fmt.Printf("[Gemini][WARN] Provider %s 配置验证失败，已自动跳过: %v\n", p.Name, errs)
+         continue
+     }
+
+     // 【新增】模型支持检查
+     if requestedModel != "" && !p.IsModelSupported(requestedModel) {
+         fmt.Printf("[Gemini][INFO] Provider %s 不支持模型 %s，已跳过\n", p.Name, requestedModel)
+         continue
+     }

      // ... 其他检查 ...
      activeProviders = append(activeProviders, p)
  }
```

**Step 5: 修改 forwardGeminiRequest 增加模型映射**

文件: `services/providerrelay.go:1472` (在构建 targetURL 之前)

```diff
+ // 【新增】模型映射
+ effectiveModel := provider.GetEffectiveModel(extractGeminiModelFromEndpoint(endpoint))
+ originalModel := extractGeminiModelFromEndpoint(endpoint)
+ if effectiveModel != originalModel && originalModel != "" {
+     fmt.Printf("[Gemini] Provider %s 映射模型: %s -> %s\n", provider.Name, originalModel, effectiveModel)
+     newEndpoint, err := replaceGeminiModelInEndpoint(endpoint, originalModel, effectiveModel)
+     if err != nil {
+         fmt.Printf("[Gemini][WARN] 模型映射替换失败: %v，使用原 endpoint\n", err)
+     } else {
+         endpoint = newEndpoint
+     }
+ }

  // 构建目标 URL
  targetURL := strings.TrimSuffix(provider.BaseURL, "/") + endpoint
```

#### 2.2.3 验收标准

- [ ] GeminiProvider 新增 `SupportedModels` 和 `ModelMapping` 字段
- [ ] 未配置白名单时假设支持所有模型（向后兼容）
- [ ] 配置白名单后，不支持的模型会跳过该 provider
- [ ] 模型映射能正确替换 URL 中的模型名
- [ ] 通配符匹配正常工作（如 `gemini-*`）

---

### 2.3 P2: 客户端中断不计失败

#### 2.3.1 问题分析

**当前问题代码位置**: `services/providerrelay.go:1406-1414`

```go
// 【问题】没有检测客户端中断，所有失败都记录到黑名单
if responseWritten {
    _ = prs.blacklistService.RecordFailure("gemini", provider.Name)
    return
}
```

**参考实现**: `services/providerrelay.go:644-649` (Claude/Codex)

```go
if errors.Is(err, errClientAbort) {
    fmt.Printf("[INFO] 客户端中断，跳过失败计数: %s\n", provider.Name)
} else if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
    // ...
}
```

#### 2.3.2 修复方案

**Step 1: 修改 forwardGeminiRequest 函数签名**

文件: `services/providerrelay.go:1461-1468`

```diff
  func (prs *ProviderRelayService) forwardGeminiRequest(
      c *gin.Context,
      provider *GeminiProvider,
      endpoint string,
      bodyBytes []byte,
      isStream bool,
      requestLog *ReqeustLog,
- ) (success bool, errMsg string, responseWritten bool) {
+ ) (success bool, err error, responseWritten bool) {
```

**Step 2: 新增客户端中断检测函数**

文件: `services/providerrelay.go` (新增在 errClientAbort 定义附近)

```go
// isClientAbortError 检测是否为客户端中断错误
func isClientAbortError(err error) bool {
    if err == nil {
        return false
    }
    errStr := err.Error()
    return strings.Contains(errStr, "broken pipe") ||
           strings.Contains(errStr, "connection reset") ||
           strings.Contains(errStr, "client disconnected") ||
           strings.Contains(errStr, "use of closed network connection")
}
```

**Step 3: 修改 forwardGeminiRequest 内部错误处理**

文件: `services/providerrelay.go` (流式传输中断检测处)

```diff
  copyErr := streamGeminiResponseWithHook(resp.Body, c.Writer, requestLog)
  if copyErr != nil {
+     // 【新增】检测客户端中断（写入错误通常表示客户端断开）
+     if isClientAbortError(copyErr) {
+         fmt.Printf("[Gemini]   ⚠️ 客户端中断: %s | 错误: %v\n", provider.Name, copyErr)
+         return false, fmt.Errorf("%w: %v", errClientAbort, copyErr), true
+     }
-     return false, fmt.Sprintf("流式传输中断: %v", copyErr), true
+     return false, fmt.Errorf("流式传输中断: %v", copyErr), true
  }
```

**Step 4: 修改 geminiProxyHandler 调用方**

文件: `services/providerrelay.go` (geminiProxyHandler 内降级模式部分)

```diff
- ok, errMsg, responseWritten := prs.forwardGeminiRequest(c, &provider, endpoint, bodyBytes, isStream, requestLog)
+ ok, err, responseWritten := prs.forwardGeminiRequest(c, &provider, endpoint, bodyBytes, isStream, requestLog)
  if ok {
      _ = prs.blacklistService.RecordSuccess("gemini", provider.Name)
      prs.setLastUsedProvider("gemini", provider.Name)
      return
  }

  if responseWritten {
-     _ = prs.blacklistService.RecordFailure("gemini", provider.Name)
+     // 【修复】客户端中断不计入失败次数
+     if errors.Is(err, errClientAbort) {
+         fmt.Printf("[Gemini] 客户端中断，跳过失败计数: %s\n", provider.Name)
+     } else {
+         _ = prs.blacklistService.RecordFailure("gemini", provider.Name)
+     }
      return
  }

- lastError = errMsg
+ errMsg := "未知错误"
+ if err != nil {
+     errMsg = err.Error()
+ }
+ lastError = errMsg
+
+ // 【修复】普通失败也检查客户端中断
+ if errors.Is(err, errClientAbort) {
+     fmt.Printf("[Gemini] 客户端中断，跳过失败计数: %s\n", provider.Name)
+ } else {
      _ = prs.blacklistService.RecordFailure("gemini", provider.Name)
+ }
```

#### 2.3.3 验收标准

- [ ] 客户端中断（broken pipe）不触发 RecordFailure
- [ ] 日志明确输出"客户端中断，跳过失败计数"
- [ ] 函数签名从 `(bool, string, bool)` 改为 `(bool, error, bool)`
- [ ] 使用 `errors.Is(err, errClientAbort)` 检测（与 Claude/Codex 一致）

---

### 2.4 P2: 日志落库粒度对齐

#### 2.4.1 问题分析

**当前问题代码位置**: `services/providerrelay.go:1236-1265`

```go
// Gemini 只在外层创建一个 requestLog，多 provider 降级时中间失败不落库
requestLog := &ReqeustLog{
    Platform: "gemini",
    // ...
}
defer func() {
    // 整次请求结束时落库
}()
```

**参考实现**: Claude/Codex 每次尝试都创建独立日志 (`services/providerrelay.go:734-777`)

#### 2.4.2 修复方案

**Step 1: 移除外层 requestLog 创建**

文件: `services/providerrelay.go` (geminiProxyHandler 内)

```diff
- // 创建 requestLog（整次请求一条）
- requestLog := &ReqeustLog{
-     Platform: "gemini",
-     // ...
- }
- defer func() {
-     // 落库逻辑
- }()
```

**Step 2: 在 forwardGeminiRequest 内部创建日志**

文件: `services/providerrelay.go:1469` (函数开头)

```diff
  func (prs *ProviderRelayService) forwardGeminiRequest(
      c *gin.Context,
      provider *GeminiProvider,
      endpoint string,
      bodyBytes []byte,
      isStream bool,
-     requestLog *ReqeustLog,
  ) (success bool, err error, responseWritten bool) {
      providerStart := time.Now()

+     // 【新增】每次尝试创建独立日志
+     requestLog := &ReqeustLog{
+         Platform: "gemini",
+         Provider: provider.Name,
+         Model:    extractGeminiModelFromEndpoint(endpoint),
+         IsStream: isStream,
+     }
+     if requestLog.Model == "" {
+         requestLog.Model = provider.Model
+     }
+
+     // 【新增】使用 defer 确保落库
+     defer func() {
+         requestLog.DurationSec = time.Since(providerStart).Seconds()
+         if GlobalDBQueueLogs == nil {
+             return
+         }
+         ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
+         defer cancel()
+         _ = GlobalDBQueueLogs.ExecBatchCtx(ctx, `
+             INSERT INTO request_log (
+                 platform, provider, model, http_code, is_stream,
+                 input_tokens, output_tokens, reasoning_tokens,
+                 cache_creation_input_tokens, cache_read_input_tokens,
+                 duration_sec
+             ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
+         `,
+             requestLog.Platform,
+             requestLog.Provider,
+             requestLog.Model,
+             requestLog.HttpCode,
+             requestLog.IsStream,
+             requestLog.InputTokens,
+             requestLog.OutputTokens,
+             requestLog.ReasoningTokens,
+             requestLog.CacheCreationInputTokens,
+             requestLog.CacheReadInputTokens,
+             requestLog.DurationSec,
+         )
+     }()

      // ... 其余逻辑 ...
  }
```

**Step 3: 修改调用方移除 requestLog 参数**

文件: `services/providerrelay.go` (所有调用 forwardGeminiRequest 的地方)

```diff
- ok, err, responseWritten := prs.forwardGeminiRequest(c, &provider, endpoint, bodyBytes, isStream, requestLog)
+ ok, err, responseWritten := prs.forwardGeminiRequest(c, &provider, endpoint, bodyBytes, isStream)
```

#### 2.4.3 验收标准

- [ ] Gemini 降级时每个 provider 尝试都生成独立日志
- [ ] 热力图、成本统计口径与 Claude/Codex 一致（按尝试统计）
- [ ] 函数签名移除 `requestLog *ReqeustLog` 参数

---

### 2.5 P3: 切换通知补齐

#### 2.5.1 问题分析

**当前问题**: Gemini 降级失败时没有调用 `NotifyProviderSwitch`

**参考实现**: `services/providerrelay.go:651-674` (Claude/Codex)

#### 2.5.2 修复方案

**Step 1: 在降级循环中添加切换通知**

文件: `services/providerrelay.go` (geminiProxyHandler 降级模式循环内，失败分支)

```diff
  // 失败处理
+ lastError = errMsg
+
+ // 【新增】发送切换通知（与 Claude/Codex 一致）
+ if prs.notificationService != nil {
+     nextProvider := ""
+     // 查找同级别的下一个 provider
+     if idx+1 < len(providersInLevel) {
+         nextProvider = providersInLevel[idx+1].Name
+     } else {
+         // 查找下一个 level 的第一个 provider
+         for _, nextLevel := range sortedLevels {
+             if nextLevel > level && len(levelGroups[nextLevel]) > 0 {
+                 nextProvider = levelGroups[nextLevel][0].Name
+                 break
+             }
+         }
+     }
+     if nextProvider != "" {
+         prs.notificationService.NotifyProviderSwitch(SwitchNotification{
+             FromProvider: provider.Name,
+             ToProvider:   nextProvider,
+             Reason:       errMsg,
+             Platform:     "gemini",
+         })
+     }
+ }

  // 记录失败
  if !errors.Is(err, errClientAbort) {
      _ = prs.blacklistService.RecordFailure("gemini", provider.Name)
  }
```

#### 2.5.3 验收标准

- [ ] Gemini 降级切换时发送系统通知
- [ ] 通知内容包含: FromProvider, ToProvider, Reason, Platform
- [ ] 与 Claude/Codex 的通知格式一致

---

## 3. 实施顺序与依赖

```
P0 (Enabled 语义) ─┬─> P1 (模型白名单)
                   │
                   └─> P2 (客户端中断) ─> P2 (日志粒度) ─> P3 (切换通知)
```

**推荐实施顺序**:
1. P0: Enabled 语义冲突（独立，无依赖）
2. P2: 客户端中断（需修改函数签名，影响后续）
3. P2: 日志落库粒度（依赖函数签名变更）
4. P1: 模型白名单/映射（独立，可并行）
5. P3: 切换通知（最后补齐）

---

## 4. 风险评估

| 风险项 | 影响 | 缓解措施 |
|--------|------|----------|
| 函数签名变更导致编译错误 | 中 | 一次性修改所有调用方 |
| 日志统计口径变化 | 低 | 口径与 Claude/Codex 对齐，本质是修复 |
| OAuth 唯一性限制 | 低 | 大多数用户只有一个 Google Official |
| 向后兼容性 | 低 | 新字段使用 omitempty，旧配置可正常加载 |

---

## 5. 验收清单

Codex 验收时需检查以下各项：

### 5.1 P0 验收
- [ ] `SwitchProvider` 不再修改其他 provider 的 `Enabled`
- [ ] `GetDirectAppliedProviderID` 对 OAuth 做唯一性检查
- [ ] 多 OAuth provider 时返回明确错误

### 5.2 P1 验收
- [ ] `GeminiProvider` 新增 `SupportedModels` 和 `ModelMapping` 字段
- [ ] `IsModelSupported` 方法正确实现
- [ ] `GetEffectiveModel` 方法正确实现
- [ ] `replaceGeminiModelInEndpoint` 函数安全替换 URL 中模型名
- [ ] geminiProxyHandler 正确过滤不支持的 provider

### 5.3 P2 验收（客户端中断）
- [ ] 函数签名从 `(bool, string, bool)` 改为 `(bool, error, bool)`
- [ ] 使用 `errors.Is(err, errClientAbort)` 检测
- [ ] 客户端中断不触发 `RecordFailure`

### 5.4 P2 验收（日志粒度）
- [ ] 每次 provider 尝试都生成独立日志
- [ ] 外层 requestLog 创建已移除
- [ ] defer 落库逻辑在 `forwardGeminiRequest` 内部

### 5.5 P3 验收
- [ ] 降级切换时调用 `NotifyProviderSwitch`
- [ ] 通知包含 FromProvider, ToProvider, Reason, Platform

---

## 6. 附录

### 6.1 相关文件列表

| 文件 | 修改类型 | 描述 |
|------|----------|------|
| `services/geminiservice.go` | 修改 | GeminiProvider 结构体扩展、方法新增、SwitchProvider 修复 |
| `services/providerrelay.go` | 修改 | forwardGeminiRequest 签名变更、模型过滤/映射、日志落库、切换通知 |

### 6.2 测试用例建议

1. **P0 测试**: 创建多个 provider，切换直连后验证其他 provider 的 Enabled 状态
2. **P1 测试**: 配置模型白名单，发送不支持的模型请求，验证跳过行为
3. **P2 测试**: 模拟客户端中断，验证不触发黑名单
4. **P3 测试**: 触发降级，验证系统通知发送

---

**文档结束**
