# Project Overview

> **Doc Maintenance**: Keep concise, avoid redundancy, clean up outdated content promptly to reduce AI context usage.
> **Scope**: This document reflects the current codebase state only and does not describe future plans.
> **Goal**: Help AI quickly locate relevant code by module, type, and data flow.

**AI Coding Assistant Proxy Manager** - A desktop application that manages API providers for Claude Code, Codex, and Gemini CLI. Features include multi-provider failover, model mapping, usage statistics, cost tracking, blacklist management, and MCP server configuration.

## Tech Stack

| Layer | Technology |
|-------|------------|
| Framework | [Wails 3](https://v3.wails.io) |
| Backend | Go 1.24 + Gin + SQLite |
| Frontend | Vue 3 + TypeScript + Tailwind CSS |
| Packaging | NSIS (Windows) / nFPM (Linux) |

## Module Map

| Module | Purpose |
|--------|---------|
| `main.go` | App entry point, Wails app initialization, service registration, system tray, update recovery/cleanup |
| `services/providerservice.go` | Provider CRUD, model whitelist/mapping validation, wildcard matching, configuration migration |
| `services/providerrelay.go` | Core proxy server (:18100), request forwarding, Level-based failover, blacklist/round-robin modes, SSE streaming, token parsing |
| `services/blacklistservice.go` | Provider blacklist management, Level-based blacklist (L1-L5), auto-recovery, forgiveness mechanism |
| `services/geminiservice.go` | Gemini provider management, preset providers, OAuth/API-Key auth types, env/settings config |
| `services/database.go` | SQLite initialization, WAL mode, table schema (request_log, provider_blacklist, app_settings, health_check_history, hotkeys) |
| `services/dbqueue.go` | Async write queue for high-frequency DB writes, batch commits, concurrency control |
| `services/logservice.go` | Request log queries, model pricing integration, cost calculation |
| `services/settingsservice.go` | Blacklist config, Level config, retry config persistence |
| `services/claudesettings.go` | Claude Code config file manipulation (model, MCP, custom instructions) |
| `services/codexsettings.go` | Codex CLI config file manipulation |
| `services/cliconfigservice.go` | Generic CLI config editor, JSON path operations |
| `services/mcpservice.go` | MCP server management, sync to Claude/Codex configs |
| `services/updateservice.go` | Auto-update check, download, apply (cross-platform) |
| `services/healthcheckservice.go` | Provider availability monitoring, background polling |
| `services/speedtestservice.go` | Provider latency testing |
| `services/skillservice.go` | Claude Skills marketplace, one-click install |
| `services/promptservice.go` | Custom system prompt management |
| `services/importservice.go` | Provider/MCP config import |
| `services/deeplinkservice.go` | Deep link handling (`ccswitch://`) |
| `services/notificationservice.go` | Frontend event notifications (provider switch, blacklist) |
| `services/cache_affinity.go` | 5-minute same-origin cache affinity for provider selection |
| `services/requestdetailcache.go` | In-memory request/response detail cache |
| `services/requestdetailservice.go` | Frontend binding for request detail cache (mode control, data retrieval) |
| `services/customcliservice.go` | Custom CLI tool proxy endpoints |
| `services/networkservice.go` | Network listen address management (localhost/WSL/LAN modes) |
| `services/autostartservice.go` | OS auto-start configuration |
| `services/proxystate.go` | Proxy enable/disable state tracking |
| `services/envcheckservice.go` | Environment and dependency checks |
| `services/consoleservice.go` | Console log capture, redirect stdout/stderr to memory buffer |
| `services/servicestore.go` | Hotkey storage service |
| `services/constants.go` | Centralized API version constants (e.g., `GetAnthropicAPIVersion()`), env override support |
| `frontend/src/` | Vue 3 SPA: provider cards, logs, heatmap, settings, MCP editor |

## Key Types

```go
// Provider (core entity, stored in ~/.code-switch/{claude-code,codex}.json)
Provider {
    ID, Name, APIURL, APIKey, Enabled
    Site, Icon, Tint, Accent      // UI display fields
    Level                         // Priority group (1-10, lower = higher priority)
    APIEndpoint                   // Override default endpoint (e.g., /v1/chat/completions)
    SupportedModels               // map[string]bool - whitelist with wildcard support
    ModelMapping                  // map[string]string - external→internal model name
    AvailabilityMonitorEnabled, ConnectivityAutoBlacklist
    AvailabilityConfig            // {TestModel, TestEndpoint, Timeout}
    ConnectivityAuthType          // Auth type override (bearer/x-api-key)
    // Header config (v0.6.0+)
    ExtraHeaders                  // map[string]string - add if not exists
    OverrideHeaders               // map[string]string - force overwrite
    StripHeaders                  // []string - headers to remove before forwarding
}

// GeminiProvider (stored in ~/.code-switch/gemini.json)
GeminiProvider {
    ID, Name, BaseURL, APIKey, Model, Enabled, Level
    WebsiteURL, APIKeyURL, Description  // Provider info
    Category                      // official, third_party, custom
    PartnerPromotionKey           // Vendor identification
    EnvConfig, SettingsConfig     // Gemini CLI specific configs
}

// Request processing
RequestLog {
    ID, Platform, CreatedAt       // Platform: claude, codex, gemini, custom:*
    Model, Provider, HttpCode
    InputTokens, OutputTokens, CacheCreateTokens, CacheReadTokens, ReasoningTokens
    IsStream, DurationSec
    // Costs (computed via model pricing)
    InputCost, OutputCost, ReasoningCost
    CacheCreateCost, CacheReadCost, Ephemeral5mCost, Ephemeral1hCost
    TotalCost, HasPricing
    RequestDetailID               // Link to in-memory detail cache
}

// Blacklist management
BlacklistStatus {
    Platform, ProviderName, FailureCount
    BlacklistedAt, BlacklistedUntil, LastFailureAt, IsBlacklisted
    RemainingSeconds              // Time left in blacklist (seconds)
    BlacklistLevel                // L1-L5 (L0 = not blacklisted)
    LastRecoveredAt               // Last recovery timestamp
    ForgivenessRemaining          // Countdown to level reset (seconds)
}

// Level blacklist config (v0.4.0+)
BlacklistLevelConfig {
    EnableLevelBlacklist          // false = fixed mode (simple threshold)
    FailureThreshold              // Failures before blacklist (default: 3)
    DedupeWindowSeconds           // Dedupe window (default: 2)
    RetryWaitSeconds              // Same-provider retry wait (default: 3)
    // Level durations (minutes): L1=5, L2=15, L3=60, L4=360, L5=1440
    L1-L5DurationMinutes          // Configurable per-level durations
    NormalDegradeIntervalHours    // Hours per level decay (default: 1)
    ForgivenessHours              // Stable hours to reset to L0 (default: 3)
    JumpPenaltyWindowHours        // Jump penalty window (default: 2.5)
    FallbackMode                  // "fixed" or "none" when disabled
    FallbackDurationMinutes       // Fixed mode duration (default: 30)
}

// Proxy modes
AuthMethod: AuthMethodBearer | AuthMethodXAPIKey  // Detect and preserve original auth
```

## Data Flow

### Proxy Request Pipeline

```
CLI (Claude/Codex/Gemini)
    ↓
Local Proxy (:18100)
    ├── /v1/messages              → Claude handler
    ├── /v1/models                → OpenAI-compatible models endpoint
    ├── /responses                → Codex handler
    ├── /gemini/v1beta/*          → Gemini handler
    ├── /gemini/v1/*              → Gemini handler (alternative)
    └── /custom/:toolId/v1/messages → Custom CLI handler
    ↓
Load providers (kind=claude|codex|gemini|custom:*)
    ↓
Filter: Enabled + URL + APIKey + Model whitelist + Not blacklisted
    ↓
[5-min affinity cache hit?] → Try cached provider first
    ↓
Group by Level → Sort ascending (1→10)
    ↓
[Round-robin enabled?] → Rotate within same Level
    ↓
For each Level:
    For each Provider:
        ├── Model mapping: GetEffectiveModel()
        ├── Endpoint override: GetEffectiveEndpoint()
        ├── Auth method: preserve original (Bearer/x-api-key)
        ├── Forward request (with network-level retry)
        │   ├── Success → Update affinity cache, RecordSuccess(), return
        │   └── Fail → RecordFailure(), try next
        └── [Blacklist mode] → Retry same provider until blacklisted, then switch
    ↓
All failed → 502 Bad Gateway
```

### Blacklist Recovery Flow

```
Provider failure
    ↓
RecordFailure() → Increment failure_count (with dedupe window)
    ↓
[failure_count >= threshold?]
    ├── No  → Continue
    └── Yes → Blacklist provider
              ├── [Level mode] → Escalate level (L0→L1→...→L5)
              │                  Duration = L{n}DurationMinutes config
              └── [Fixed mode] → FallbackDurationMinutes
    ↓
Background ticker (1 min)
    ↓
AutoRecoverExpired() → Check blacklisted_until
    ├── Expired → Clear blacklist, set last_recovered_at
    └── Not expired → Skip
    ↓
Next success: RecordSuccess()
    ├── [Level mode] → Degrade level (NormalDegradeIntervalHours)
    │                  [ForgivenessHours stable] → Forgiveness → L0
    └── [Fixed mode] → Just clear failure_count
```

### Token Usage Parsing

```
Streaming response (SSE)
    ↓
RequestLogHook() → Parse each chunk
    ├── Claude: usage.input_tokens, usage.output_tokens, cache_*_tokens
    ├── Codex:  response.usage.*, reasoning_tokens
    └── Gemini: usageMetadata.* (take max, not sum)
    ↓
Write to request_log (via DBQueueLogs batch)
    ↓
LogService.ListRequestLogs()
    ├── decorateCost() → Apply model pricing
    └── matchDetailID() → Link to RequestDetailCache
```

### Config File Locations

```
~/.code-switch/
├── claude-code.json       # Claude providers
├── codex.json             # Codex providers
├── gemini.json            # Gemini providers
├── providers/             # Custom CLI providers: {toolId}.json
├── mcp.json               # MCP server configs
├── app.db                 # SQLite: request_log, provider_blacklist, app_settings, health_check_history, hotkeys
├── updates/               # Downloaded update files
├── blacklist-config.json  # Level blacklist configuration
├── skill.json             # Skills configuration
├── prompts.json           # Custom system prompts
├── custom-cli.json        # Custom CLI tool definitions
├── cli-templates.json     # CLI config templates
├── app.json               # Application settings
├── proxy-state/           # Proxy state per platform (claude.json, codex.json, etc.)
└── icons/                 # Notification icon cache
```

### Proxy Activation Flow

```
User toggles "Proxy ON"
    ↓
ClaudeSettingsService.EnableProxy() / CodexSettingsService.EnableProxy()
    ↓
Locate config file: ~/.claude/settings.json or ~/.codex/config.json
    ↓
Inject apiUrl: "http://127.0.0.1:18100/v1/messages" (Claude)
       apiUrl: "http://127.0.0.1:18100/responses" (Codex)
    ↓
CLI tools now route through local proxy
```
