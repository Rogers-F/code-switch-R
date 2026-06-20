# 88code 连通性测试研究报告

## 背景

88code (`https://88code.wu.ren/api`) 是一个 Claude API 代理服务，但它有严格的请求来源检测机制，会拒绝非 Claude Code 客户端的请求。

**错误信息**：
```json
{"error":{"code":400,"message":"暂不支持非 claude code 请求","type":"Bad Request"},"type":"error"}
```

## 研究过程

### 1. 初始测试

使用标准 Claude Messages API 请求格式：
```json
{
  "model": "claude-haiku-4-5-20251001",
  "max_tokens": 10,
  "messages": [{"role": "user", "content": "hi"}]
}
```
**结果**：❌ 被拒绝

### 2. 添加 system prompt

参考 ClaudeChrome++ 插件的兼容模式，在 system 首位插入 Claude Code 标识：
```json
{
  "system": [{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."}],
  ...
}
```
**结果**：❌ 仍被拒绝

### 3. 添加 betas 参数

发现插件使用了 `betas: ["oauth-2025-04-20"]` 参数：
```json
{
  "betas": ["oauth-2025-04-20"],
  "system": [{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."}],
  ...
}
```
**结果**：❌ 仍被拒绝

### 4. 添加 metadata 参数

最终发现需要 `metadata` 参数：
```json
{
  "betas": ["oauth-2025-04-20"],
  "system": [{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."}],
  "metadata": {"user_id": "user_unknown_account__session_unknown"},
  ...
}
```
**结果**：✅ 成功！

## 88code 检测机制

88code 服务端通过检查请求体中的特定字段组合来判断请求是否来自"合法"客户端。

### 必需参数（缺一不可）

| 参数 | 格式 | 说明 |
|------|------|------|
| `system` | `[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude."}]` | 必须是数组格式，首位必须是 Claude Code 标识 |
| `betas` | `["oauth-2025-04-20"]` | OAuth beta 标识 |
| `metadata` | `{"user_id":"..."}` | 用户标识，值可以是任意字符串 |

### 最小有效请求

```json
{
  "model": "claude-haiku-4-5-20251001",
  "max_tokens": 10,
  "stream": true,
  "betas": ["oauth-2025-04-20"],
  "system": [
    {"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."}
  ],
  "messages": [
    {"role": "user", "content": "hi"}
  ],
  "metadata": {
    "user_id": "test"
  }
}
```

### 请求头

```
Content-Type: application/json
Authorization: Bearer <api_key>
anthropic-version: 2023-06-01
```

## 测试验证矩阵

| system prompt | betas | metadata | 结果 |
|---------------|-------|----------|------|
| ❌ | ❌ | ❌ | 400 |
| ✅ | ❌ | ❌ | 400 |
| ❌ | ✅ | ❌ | 400 |
| ✅ | ✅ | ❌ | 400 |
| ❌ | ❌ | ✅ | 400 |
| ✅ | ❌ | ✅ | 400 |
| ❌ | ✅ | ✅ | 400 |
| ✅ | ✅ | ✅ | **200** |

## 参考来源

发现来自 **ClaudeChrome++** 插件（位于 `C:\Users\Administrator.DESKTOP-BR3KL51\Downloads\神秘插件已更新\ClaudeChrome++`）：

### 兼容模式配置
```javascript
// i18n/zh-CN.json
"compatModeTitle": "兼容模式",
"compatModeLabel": "启用 Claude Code 限制提示词检查兼容",
"compatModeDesc": "在系统提示词首位插入 \"You are Claude Code, Anthropic's official CLI for Claude.\"。某些代理服务商需要此设置。"
```

### 核心实现
```javascript
// assets/sidepanel-CCjzuRfG.js
const compatModeEnabled = await J(G.COMPAT_MODE);
if (compatModeEnabled) {
    a.unshift({
        type: "text",
        text: "You are Claude Code, Anthropic's official CLI for Claude."
    });
}

// API 请求构建
const b = {
    ...y,
    messages: C,
    max_tokens: f,
    model: x,
    betas: ["oauth-2025-04-20"]
};

// metadata 添加
b.metadata = {
    user_id: `user_${o??"unknown"}_account_${r??""}_session_${i??"unknown"}`
};

// 发送请求
await t.beta.messages.create(b, p)
```

## 对 Code-Switch 项目的影响

### 当前连通性测试服务

当前 `ConnectivityTestService` 使用简单请求体：
```go
reqBody := map[string]interface{}{
    "model":      model,
    "max_tokens": 1,
    "messages": []map[string]string{
        {"role": "user", "content": "hi"},
    },
}
```

**问题**：无法通过 88code 等有请求来源检测的服务。

### 建议改进

1. **Provider 级别配置**：新增 `compatibilityMode` 字段
2. **自动注入参数**：当启用兼容模式时，自动添加必需参数
3. **连通性测试适配**：测试时也使用兼容模式参数

### 示例 Provider 配置扩展

```json
{
  "id": 1,
  "name": "88code",
  "apiUrl": "https://88code.wu.ren/api",
  "apiKey": "88_xxx",
  "enabled": true,
  "compatibilityMode": {
    "enabled": true,
    "injectSystemPrompt": true,
    "injectBetas": ["oauth-2025-04-20"],
    "injectMetadata": true
  }
}
```

## 测试脚本

完整测试脚本：`test_88code_connectivity.go`

```bash
cd G:\claude-lit\cc-r
go run test_88code_connectivity.go
```

## 日期

2024-12-30
