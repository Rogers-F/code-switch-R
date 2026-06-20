# Skill Management Feature Implementation Document

**版本**: 1.0
**日期**: 2025-12-29
**作者**: Claude + Codex 协作

---

## 1. 功能概述

### 1.1 目标

复现 Claude Code 原生设置面板中的 Skills 管理功能，并扩展支持：

1. **多平台支持**: Claude Code + Codex
2. **多位置安装**: 用户级 (`~/.claude/skills/`) + 项目级 (`./.claude/skills/`)
3. **开关控制**: 通过修改 `SKILL.md` front matter 实现启用/禁用
4. **内容查看**: 可展开查看 skill 的完整 markdown 内容
5. **文件夹操作**: 打开 skills 目录

### 1.2 核心用户故事

1. 作为用户，我希望能在一个界面管理 Claude Code 和 Codex 的技能
2. 作为用户，我希望能选择将技能安装到用户级或项目级目录
3. 作为用户，我希望能通过开关快速启用/禁用技能
4. 作为用户，我希望能查看已安装技能的内容
5. 作为用户，我希望能快速打开技能所在目录

---

## 2. 技术设计

### 2.1 Claude Code Skills 机制分析

**关键发现（来源: Codex 对 Claude Code 源码的分析）**:

```javascript
// cli.js:2485 - 禁用模型调用
disable-model-invocation: true  // 阻止 Claude 执行该 skill

// cli.js:2489 - 禁用用户调用
user-invocable: false  // 阻止用户通过 /skill 调用

// cli.js:3905 - 扫描路径
~/.claude/skills/      // 用户级
./.claude/skills/      // 项目级

// cli.js:4289 - Skill 类定义
class Skill { name, description, disableModelInvocation, userInvocable, ... }
```

**核心语义映射**:
- `enabled=true` → `disable-model-invocation: false`（允许 Claude 调用）
- `enabled=false` → `disable-model-invocation: true`（禁止 Claude 调用）

### 2.2 目录结构设计

```
Claude Code:
  用户级: ~/.claude/skills/{skill-name}/SKILL.md
  项目级: ./.claude/skills/{skill-name}/SKILL.md

Codex (假设类似结构):
  用户级: ~/.codex/skills/{skill-name}/SKILL.md
  项目级: ./.codex/skills/{skill-name}/SKILL.md
```

### 2.3 数据结构扩展

#### 后端 Skill 结构 (`services/skillservice.go`)

```go
type Skill struct {
    Key             string `json:"key"`
    Name            string `json:"name"`
    Description     string `json:"description"`
    Directory       string `json:"directory"`
    ReadmeURL       string `json:"readme_url"`
    Installed       bool   `json:"installed"`

    // 新增字段
    Enabled         bool   `json:"enabled"`                   // 是否启用（从 SKILL.md 读取）
    LicenseFile     string `json:"license_file,omitempty"`    // 许可证文件路径
    Platform        string `json:"platform,omitempty"`        // "claude" | "codex"
    InstallLocation string `json:"install_location,omitempty"` // "user" | "project"

    // 现有仓库字段
    RepoOwner       string `json:"repo_owner,omitempty"`
    RepoName        string `json:"repo_name,omitempty"`
    RepoBranch      string `json:"repo_branch,omitempty"`
}
```

#### 前端 SkillSummary 类型 (`frontend/src/services/skill.ts`)

```typescript
export type SkillSummary = {
  key: string
  name: string
  description: string
  directory: string
  readme_url: string
  installed: boolean

  // 新增字段
  enabled: boolean
  license_file?: string
  platform: 'claude' | 'codex' | ''
  install_location: 'user' | 'project' | ''

  // 现有仓库字段
  repo_owner?: string
  repo_name?: string
  repo_branch?: string
}
```

---

## 3. 后端实现详情

### 3.1 文件: `services/skillservice.go`

#### 3.1.1 新增常量

```go
const (
    // 现有常量
    skillStoreDir  = ".code-switch"
    skillStoreFile = "skill.json"

    // 新增常量
    skillPlatformClaude = "claude"
    skillPlatformCodex  = "codex"
    skillLocationUser   = "user"
    skillLocationProject = "project"
)
```

#### 3.1.2 新增辅助函数: `getInstallPath`

**位置**: 在 `NewSkillService` 之后添加

```go
// getInstallPath 根据平台和位置返回 skills 目录路径
// platform: "claude" | "codex"
// location: "user" | "project"
func (ss *SkillService) getInstallPath(platform, location string) (string, error) {
    var basePath string

    switch location {
    case skillLocationProject:
        // 项目级: 使用当前工作目录
        cwd, err := os.Getwd()
        if err != nil {
            return "", fmt.Errorf("获取工作目录失败: %w", err)
        }
        basePath = cwd
    case skillLocationUser:
        fallthrough
    default:
        // 用户级: 使用 home 目录
        home, err := os.UserHomeDir()
        if err != nil {
            return "", fmt.Errorf("获取用户目录失败: %w", err)
        }
        basePath = home
    }

    var configDir string
    switch platform {
    case skillPlatformCodex:
        configDir = ".codex"
    case skillPlatformClaude:
        fallthrough
    default:
        configDir = ".claude"
    }

    return filepath.Join(basePath, configDir, "skills"), nil
}
```

#### 3.1.3 新增方法: `ListSkillsForPlatform`

**位置**: 在 `ListSkills` 之后添加

```go
// ListSkillsForPlatform 列出指定平台的技能（用户级 + 项目级）
func (ss *SkillService) ListSkillsForPlatform(platform string) ([]Skill, error) {
    if platform == "" {
        platform = skillPlatformClaude
    }

    var allSkills []Skill

    // 扫描用户级目录
    userPath, err := ss.getInstallPath(platform, skillLocationUser)
    if err == nil {
        userSkills := ss.scanSkillsDirectory(userPath, platform, skillLocationUser)
        allSkills = append(allSkills, userSkills...)
    }

    // 扫描项目级目录
    projectPath, err := ss.getInstallPath(platform, skillLocationProject)
    if err == nil {
        projectSkills := ss.scanSkillsDirectory(projectPath, platform, skillLocationProject)
        allSkills = append(allSkills, projectSkills...)
    }

    // 按名称排序
    sort.SliceStable(allSkills, func(i, j int) bool {
        return strings.ToLower(allSkills[i].Name) < strings.ToLower(allSkills[j].Name)
    })

    return allSkills, nil
}

// scanSkillsDirectory 扫描目录中的技能
func (ss *SkillService) scanSkillsDirectory(dir, platform, location string) []Skill {
    var skills []Skill

    entries, err := os.ReadDir(dir)
    if err != nil {
        return skills
    }

    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }

        skillPath := filepath.Join(dir, entry.Name())
        skillMDPath := filepath.Join(skillPath, "SKILL.md")

        // 检查 SKILL.md 是否存在
        if _, err := os.Stat(skillMDPath); err != nil {
            continue
        }

        // 读取元数据
        meta, enabled, err := ss.readSkillMetadataExtended(skillPath)
        if err != nil {
            continue
        }

        name := strings.TrimSpace(meta.Name)
        if name == "" {
            name = entry.Name()
        }

        // 检查 LICENSE 文件
        licenseFile := ""
        for _, lf := range []string{"LICENSE", "LICENSE.txt", "LICENSE.md"} {
            if _, err := os.Stat(filepath.Join(skillPath, lf)); err == nil {
                licenseFile = lf
                break
            }
        }

        skill := Skill{
            Key:             fmt.Sprintf("%s:%s:%s", platform, location, entry.Name()),
            Name:            name,
            Description:     strings.TrimSpace(meta.Description),
            Directory:       entry.Name(),
            Installed:       true,
            Enabled:         enabled,
            LicenseFile:     licenseFile,
            Platform:        platform,
            InstallLocation: location,
        }

        skills = append(skills, skill)
    }

    return skills
}
```

#### 3.1.4 新增方法: `readSkillMetadataExtended`

**位置**: 在 `readSkillMetadata` 之后添加

```go
// skillMetadataExtended 扩展的元数据结构
type skillMetadataExtended struct {
    Name                   string `yaml:"name"`
    Description            string `yaml:"description"`
    DisableModelInvocation bool   `yaml:"disable-model-invocation"`
    UserInvocable          *bool  `yaml:"user-invocable"`
}

// readSkillMetadataExtended 读取技能元数据（包括 enabled 状态）
func (ss *SkillService) readSkillMetadataExtended(dir string) (skillMetadataExtended, bool, error) {
    data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
    if err != nil {
        return skillMetadataExtended{}, false, err
    }

    meta, err := parseSkillMetadataExtended(string(data))
    if err != nil {
        return skillMetadataExtended{}, false, err
    }

    // enabled = NOT disable-model-invocation
    enabled := !meta.DisableModelInvocation

    return meta, enabled, nil
}

// parseSkillMetadataExtended 解析扩展元数据
func parseSkillMetadataExtended(content string) (skillMetadataExtended, error) {
    var meta skillMetadataExtended
    content = strings.TrimLeft(content, "\ufeff")
    parts := strings.SplitN(content, "---", 3)
    if len(parts) < 3 {
        return meta, errors.New("SKILL.md 缺少 front matter")
    }
    frontMatter := strings.TrimSpace(parts[1])
    if err := yaml.Unmarshal([]byte(frontMatter), &meta); err != nil {
        return meta, err
    }
    return meta, nil
}
```

#### 3.1.5 新增方法: `ToggleSkill`

**位置**: 在 `UninstallSkill` 之后添加

```go
// ToggleSkill 切换技能的启用状态
// 通过修改 SKILL.md 的 disable-model-invocation 字段实现
func (ss *SkillService) ToggleSkill(directory, platform, location string, enabled bool) error {
    if directory == "" {
        return errors.New("skill directory 不能为空")
    }

    installPath, err := ss.getInstallPath(platform, location)
    if err != nil {
        return err
    }

    skillMDPath := filepath.Join(installPath, directory, "SKILL.md")

    // 读取文件
    data, err := os.ReadFile(skillMDPath)
    if err != nil {
        return fmt.Errorf("读取 SKILL.md 失败: %w", err)
    }

    // 使用最小文本补丁修改
    newContent, changed, err := patchSkillFrontMatterBool(
        string(data),
        "disable-model-invocation",
        !enabled, // enabled=true → disable-model-invocation=false
    )
    if err != nil {
        return fmt.Errorf("修改 SKILL.md 失败: %w", err)
    }

    if !changed {
        return nil // 无需修改
    }

    // 原子写入
    return AtomicWriteBytes(skillMDPath, []byte(newContent))
}
```

#### 3.1.6 新增函数: `patchSkillFrontMatterBool`（YAML 最小补丁）

**位置**: 在文件末尾添加

```go
// patchSkillFrontMatterBool 最小化修改 SKILL.md 的 front matter 中的布尔字段
// 保留原有格式、注释和字段顺序
func patchSkillFrontMatterBool(markdown, key string, desired bool) (string, bool, error) {
    // 1. 保留 BOM
    hasBOM := false
    if strings.HasPrefix(markdown, "\ufeff") {
        hasBOM = true
        markdown = strings.TrimPrefix(markdown, "\ufeff")
    }

    // 2. 检测行尾风格
    lineEnding := "\n"
    if strings.Contains(markdown, "\r\n") {
        lineEnding = "\r\n"
    }

    // 3. 分割 front matter
    parts := strings.SplitN(markdown, "---", 3)
    if len(parts) < 3 {
        return "", false, errors.New("无法解析 front matter")
    }

    prefix := parts[0]      // --- 之前的内容（通常为空）
    frontMatter := parts[1] // front matter 内容
    body := parts[2]        // --- 之后的内容

    // 4. 按行处理 front matter
    lines := strings.Split(frontMatter, "\n")
    keyFound := false
    modified := false
    desiredStr := "false"
    if desired {
        desiredStr = "true"
    }

    for i, line := range lines {
        // 移除可能的 \r
        cleanLine := strings.TrimSuffix(line, "\r")
        trimmed := strings.TrimSpace(cleanLine)

        // 检查是否匹配目标 key
        if strings.HasPrefix(trimmed, key+":") {
            keyFound = true

            // 提取当前值
            colonIdx := strings.Index(trimmed, ":")
            valuePart := strings.TrimSpace(trimmed[colonIdx+1:])

            // 处理可能的行内注释
            comment := ""
            hashIdx := strings.Index(valuePart, "#")
            if hashIdx != -1 {
                comment = valuePart[hashIdx:]
                valuePart = strings.TrimSpace(valuePart[:hashIdx])
            }

            // 检查是否需要修改
            currentBool := strings.ToLower(valuePart) == "true"
            if currentBool == desired {
                continue // 值已经正确，无需修改
            }

            // 构建新行（保留原有缩进）
            indent := ""
            for _, ch := range cleanLine {
                if ch == ' ' || ch == '\t' {
                    indent += string(ch)
                } else {
                    break
                }
            }

            newLine := indent + key + ": " + desiredStr
            if comment != "" {
                newLine += " " + comment
            }

            lines[i] = newLine
            modified = true
        }
    }

    // 5. 如果 key 不存在，在 front matter 末尾插入
    if !keyFound {
        insertLine := key + ": " + desiredStr
        // 在最后一行（通常是空行）之前插入
        insertIdx := len(lines) - 1
        for insertIdx > 0 && strings.TrimSpace(lines[insertIdx]) == "" {
            insertIdx--
        }
        insertIdx++

        newLines := make([]string, 0, len(lines)+1)
        newLines = append(newLines, lines[:insertIdx]...)
        newLines = append(newLines, insertLine)
        newLines = append(newLines, lines[insertIdx:]...)
        lines = newLines
        modified = true
    }

    // 6. 重建文档
    newFrontMatter := strings.Join(lines, "\n")
    result := prefix + "---" + newFrontMatter + "---" + body

    // 7. 恢复 BOM
    if hasBOM {
        result = "\ufeff" + result
    }

    return result, modified, nil
}
```

#### 3.1.7 新增方法: `GetSkillContent`

**位置**: 在 `ToggleSkill` 之后添加

```go
// GetSkillContent 获取技能的 SKILL.md 内容
func (ss *SkillService) GetSkillContent(directory, platform, location string) (string, error) {
    if directory == "" {
        return "", errors.New("skill directory 不能为空")
    }

    installPath, err := ss.getInstallPath(platform, location)
    if err != nil {
        return "", err
    }

    skillMDPath := filepath.Join(installPath, directory, "SKILL.md")
    data, err := os.ReadFile(skillMDPath)
    if err != nil {
        return "", fmt.Errorf("读取 SKILL.md 失败: %w", err)
    }

    return string(data), nil
}
```

#### 3.1.8 新增方法: `SaveSkillContent`

```go
// SaveSkillContent 保存技能的 SKILL.md 内容
func (ss *SkillService) SaveSkillContent(directory, platform, location, content string) error {
    if directory == "" {
        return errors.New("skill directory 不能为空")
    }

    installPath, err := ss.getInstallPath(platform, location)
    if err != nil {
        return err
    }

    skillMDPath := filepath.Join(installPath, directory, "SKILL.md")

    // 原子写入
    return AtomicWriteBytes(skillMDPath, []byte(content))
}
```

#### 3.1.9 新增方法: `OpenSkillFolder`

```go
// OpenSkillFolder 打开技能目录
func (ss *SkillService) OpenSkillFolder(platform, location string) error {
    installPath, err := ss.getInstallPath(platform, location)
    if err != nil {
        return err
    }

    // 确保目录存在
    if err := os.MkdirAll(installPath, 0o755); err != nil {
        return err
    }

    return OpenInExplorer(installPath)
}
```

#### 3.1.10 修改方法: `InstallSkill`

**修改位置**: `services/skillservice.go:168`

**变更**: 添加 platform 和 location 参数支持

```go
// installRequest 扩展请求结构
type installRequest struct {
    Directory string `json:"directory"`
    RepoOwner string `json:"repo_owner"`
    RepoName  string `json:"repo_name"`
    Branch    string `json:"repo_branch"`
    Platform  string `json:"platform"`   // 新增: "claude" | "codex"
    Location  string `json:"location"`   // 新增: "user" | "project"
}

// InstallSkill 安装技能（支持多平台多位置）
func (ss *SkillService) InstallSkill(req installRequest) error {
    req.Directory = strings.TrimSpace(req.Directory)
    if req.Directory == "" {
        return errors.New("skill directory 不能为空")
    }

    // 默认值
    if req.Platform == "" {
        req.Platform = skillPlatformClaude
    }
    if req.Location == "" {
        req.Location = skillLocationUser
    }

    // 获取安装路径
    installPath, err := ss.getInstallPath(req.Platform, req.Location)
    if err != nil {
        return err
    }

    // 后续逻辑保持不变，只是使用 installPath 替代 ss.installDir
    // ... (原有逻辑)
}
```

#### 3.1.11 修改方法: `UninstallSkill`

**修改位置**: `services/skillservice.go:237`

**变更**: 添加 platform 和 location 参数

```go
// UninstallSkill 卸载技能（支持多平台多位置）
func (ss *SkillService) UninstallSkillEx(directory, platform, location string) error {
    directory = strings.TrimSpace(directory)
    if directory == "" {
        return errors.New("skill directory 不能为空")
    }

    // 默认值
    if platform == "" {
        platform = skillPlatformClaude
    }
    if location == "" {
        location = skillLocationUser
    }

    installPath, err := ss.getInstallPath(platform, location)
    if err != nil {
        return err
    }

    target := filepath.Join(installPath, directory)
    if err := os.RemoveAll(target); err != nil && !os.IsNotExist(err) {
        return err
    }

    // 更新 store（可选）
    ss.mu.Lock()
    defer ss.mu.Unlock()
    store, err := ss.loadStoreLocked()
    if err != nil {
        return err
    }
    if store.Skills == nil {
        store.Skills = make(map[string]skillState)
    }
    delete(store.Skills, directory)
    return ss.saveStoreLocked(store)
}
```

---

## 4. 前端实现详情

### 4.1 文件: `frontend/src/services/skill.ts`

#### 4.1.1 类型定义更新

```typescript
export type SkillSummary = {
  key: string
  name: string
  description: string
  directory: string
  readme_url: string
  installed: boolean

  // 新增字段
  enabled: boolean
  license_file?: string
  platform: 'claude' | 'codex' | ''
  install_location: 'user' | 'project' | ''

  repo_owner?: string
  repo_name?: string
  repo_branch?: string
}

export type InstallSkillPayload = {
  directory: string
  repo_owner?: string
  repo_name?: string
  repo_branch?: string
  platform?: 'claude' | 'codex'    // 新增
  location?: 'user' | 'project'     // 新增
}
```

#### 4.1.2 新增 API 方法

```typescript
// 获取指定平台的技能列表
export const fetchSkillsForPlatform = async (platform: 'claude' | 'codex'): Promise<SkillSummary[]> => {
  const response = await Call.ByName('codeswitch/services.SkillService.ListSkillsForPlatform', platform)
  return (response as SkillSummary[]) ?? []
}

// 切换技能启用状态
export const toggleSkill = async (
  directory: string,
  platform: string,
  location: string,
  enabled: boolean
): Promise<void> => {
  await Call.ByName('codeswitch/services.SkillService.ToggleSkill', directory, platform, location, enabled)
}

// 获取技能内容
export const getSkillContent = async (
  directory: string,
  platform: string,
  location: string
): Promise<string> => {
  const response = await Call.ByName('codeswitch/services.SkillService.GetSkillContent', directory, platform, location)
  return response as string
}

// 保存技能内容
export const saveSkillContent = async (
  directory: string,
  platform: string,
  location: string,
  content: string
): Promise<void> => {
  await Call.ByName('codeswitch/services.SkillService.SaveSkillContent', directory, platform, location, content)
}

// 打开技能文件夹
export const openSkillFolder = async (platform: string, location: string): Promise<void> => {
  await Call.ByName('codeswitch/services.SkillService.OpenSkillFolder', platform, location)
}

// 卸载技能（扩展版）
export const uninstallSkillEx = async (
  directory: string,
  platform: string,
  location: string
): Promise<void> => {
  await Call.ByName('codeswitch/services.SkillService.UninstallSkillEx', directory, platform, location)
}
```

### 4.2 文件: `frontend/src/components/Skill/Index.vue`

#### 4.2.1 UI 结构设计

```
┌─────────────────────────────────────────────────────────────┐
│  [← 返回]  Claude Skills                    [刷新] [文件夹] │
├─────────────────────────────────────────────────────────────┤
│  [Claude Code]  [Codex]                         ← 平台 Tab  │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ▼ 项目级技能 (2)                           ← 分组标题      │
│  ┌──────────────────────────────────────────┐               │
│  │ [开关] skill-name-1                [展开]│               │
│  │        ./claude/skills/skill-1           │               │
│  │        许可证: LICENSE.txt               │               │
│  └──────────────────────────────────────────┘               │
│  ┌──────────────────────────────────────────┐               │
│  │ [开关] skill-name-2                [展开]│               │
│  └──────────────────────────────────────────┘               │
│                                                              │
│  ▼ 用户级技能 (3)                                           │
│  ┌──────────────────────────────────────────┐               │
│  │ [开关] skill-name-3                [展开]│               │
│  └──────────────────────────────────────────┘               │
│  ...                                                         │
│                                                              │
│  ▼ 可用技能 (从仓库)                                        │
│  ┌──────────────────────────────────────────┐               │
│  │ skill-from-repo              [安装 ▼]   │ ← 下拉选位置   │
│  └──────────────────────────────────────────┘               │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

#### 4.2.2 关键组件状态

```typescript
// 平台选择
const activePlatform = ref<'claude' | 'codex'>('claude')

// 技能列表（分组）
const projectSkills = computed(() =>
  skills.value.filter(s => s.install_location === 'project' && s.installed)
)
const userSkills = computed(() =>
  skills.value.filter(s => s.install_location === 'user' && s.installed)
)
const availableSkills = computed(() =>
  skills.value.filter(s => !s.installed)
)

// 展开的技能
const expandedSkills = ref<Set<string>>(new Set())

// 安装模态框
const installModalOpen = ref(false)
const installTarget = ref<SkillSummary | null>(null)
const installLocation = ref<'user' | 'project'>('user')
```

#### 4.2.3 开关组件

```vue
<template>
  <Switch
    :model-value="skill.enabled"
    @update:model-value="handleToggle(skill, $event)"
    :class="[
      skill.enabled ? 'bg-blue-600' : 'bg-gray-200 dark:bg-gray-700',
      'relative inline-flex h-5 w-9 items-center rounded-full transition-colors'
    ]"
  >
    <span
      :class="[
        skill.enabled ? 'translate-x-5' : 'translate-x-1',
        'inline-block h-3 w-3 transform rounded-full bg-white transition-transform'
      ]"
    />
  </Switch>
</template>
```

#### 4.2.4 内容展开区

```vue
<template>
  <Disclosure v-slot="{ open }">
    <DisclosureButton class="skill-expand-btn">
      {{ t('components.skill.actions.viewContent') }}
      <ChevronUpIcon :class="open ? 'rotate-180' : ''" />
    </DisclosureButton>
    <transition
      enter-active-class="transition duration-100 ease-out"
      enter-from-class="transform scale-95 opacity-0"
      enter-to-class="transform scale-100 opacity-100"
      leave-active-class="transition duration-75 ease-out"
      leave-from-class="transform scale-100 opacity-100"
      leave-to-class="transform scale-95 opacity-0"
    >
      <DisclosurePanel class="skill-content-panel">
        <pre class="skill-content-pre">{{ skillContent }}</pre>
      </DisclosurePanel>
    </transition>
  </Disclosure>
</template>
```

#### 4.2.5 安装位置选择模态框

```vue
<template>
  <BaseModal :open="installModalOpen" :title="t('components.skill.install.title')" @close="closeInstallModal">
    <div class="install-modal-content">
      <p class="install-modal-desc">
        {{ t('components.skill.install.desc', { name: installTarget?.name }) }}
      </p>

      <RadioGroup v-model="installLocation">
        <RadioGroupLabel class="sr-only">
          {{ t('components.skill.install.locationLabel') }}
        </RadioGroupLabel>

        <div class="install-location-options">
          <!-- 用户级选项 -->
          <RadioGroupOption value="user" v-slot="{ checked }">
            <div :class="['install-option', { checked }]">
              <UserIcon class="install-option-icon" />
              <div>
                <p class="install-option-title">{{ t('components.skill.install.userLevel') }}</p>
                <p class="install-option-desc">~/.claude/skills/</p>
              </div>
            </div>
          </RadioGroupOption>

          <!-- 项目级选项 -->
          <RadioGroupOption value="project" v-slot="{ checked }">
            <div :class="['install-option', { checked }]">
              <FolderIcon class="install-option-icon" />
              <div>
                <p class="install-option-title">{{ t('components.skill.install.projectLevel') }}</p>
                <p class="install-option-desc">./.claude/skills/</p>
                <p class="install-option-warning">
                  ⚠️ {{ t('components.skill.install.gitWarning') }}
                </p>
              </div>
            </div>
          </RadioGroupOption>
        </div>
      </RadioGroup>

      <div class="install-modal-actions">
        <button class="btn-secondary" @click="closeInstallModal">
          {{ t('common.cancel') }}
        </button>
        <button class="btn-primary" @click="confirmInstall" :disabled="installing">
          {{ installing ? t('common.installing') : t('common.install') }}
        </button>
      </div>
    </div>
  </BaseModal>
</template>
```

---

## 5. 国际化

### 5.1 中文 (`frontend/src/locales/zh.json`)

```json
{
  "components": {
    "skill": {
      "platform": {
        "claude": "Claude Code",
        "codex": "Codex"
      },
      "groups": {
        "project": "项目级技能",
        "user": "用户级技能",
        "available": "可用技能"
      },
      "actions": {
        "toggle": "启用/禁用",
        "viewContent": "查看内容",
        "openFolder": "打开文件夹"
      },
      "install": {
        "title": "选择安装位置",
        "desc": "将 {name} 安装到：",
        "locationLabel": "安装位置",
        "userLevel": "用户级（全局）",
        "projectLevel": "项目级（当前目录）",
        "gitWarning": "项目级技能可能需要添加到 .gitignore"
      },
      "license": {
        "complete": "完整条款见 {file}"
      }
    }
  }
}
```

### 5.2 英文 (`frontend/src/locales/en.json`)

```json
{
  "components": {
    "skill": {
      "platform": {
        "claude": "Claude Code",
        "codex": "Codex"
      },
      "groups": {
        "project": "Project Skills",
        "user": "User Skills",
        "available": "Available Skills"
      },
      "actions": {
        "toggle": "Enable/Disable",
        "viewContent": "View Content",
        "openFolder": "Open Folder"
      },
      "install": {
        "title": "Choose Install Location",
        "desc": "Install {name} to:",
        "locationLabel": "Install Location",
        "userLevel": "User Level (Global)",
        "projectLevel": "Project Level (Current Directory)",
        "gitWarning": "Project skills may need to be added to .gitignore"
      },
      "license": {
        "complete": "Complete terms in {file}"
      }
    }
  }
}
```

---

## 6. 测试验证要点

### 6.1 后端测试

| 测试项 | 验证内容 | 预期结果 |
|--------|----------|----------|
| `getInstallPath` | 用户级 + 项目级路径生成 | 返回正确路径 |
| `ListSkillsForPlatform` | Claude/Codex 技能扫描 | 返回分组后的技能列表 |
| `ToggleSkill` | YAML 补丁后 front matter | `disable-model-invocation` 值正确 |
| `patchSkillFrontMatterBool` | 保留注释和格式 | 原有注释、缩进不变 |
| `GetSkillContent` | 读取 SKILL.md | 返回完整内容 |
| `OpenSkillFolder` | 打开目录 | 系统文件管理器打开 |

### 6.2 前端测试

| 测试项 | 验证内容 | 预期结果 |
|--------|----------|----------|
| 平台切换 | Tab 切换 | 加载对应平台技能 |
| 分组显示 | 项目级/用户级/可用 | 正确分组 |
| 开关切换 | Toggle 组件 | 调用后端 API |
| 内容展开 | Disclosure 组件 | 显示 SKILL.md 内容 |
| 安装模态框 | 位置选择 | 用户级/项目级正确安装 |
| 打开文件夹 | 按钮点击 | 系统文件管理器打开 |

### 6.3 边界情况

| 场景 | 处理方式 |
|------|----------|
| SKILL.md 无 front matter | 返回错误，不显示该技能 |
| `disable-model-invocation` 不存在 | 插入新字段 |
| BOM 头 | 保留 BOM |
| CRLF 行尾 | 保留 CRLF |
| 行内注释 | 保留注释 |
| 项目目录无 .claude | 自动创建 |

---

## 7. 风险评估

| 风险 | 等级 | 缓解措施 |
|------|------|----------|
| Codex 无 skills 目录结构 | 中 | 用户选择假设类似结构，运行时检查目录是否存在 |
| YAML 补丁破坏格式 | 低 | 使用最小文本补丁，只修改目标行 |
| 项目级目录权限问题 | 低 | 捕获错误并提示用户 |
| 前端状态不同步 | 低 | 每次操作后重新加载列表 |
| 大文件 SKILL.md | 低 | 限制展开区高度，添加滚动条 |

---

## 8. 实施顺序

1. **后端 `skillservice.go` 扩展**
   - 添加常量和辅助函数
   - 实现 `getInstallPath`
   - 实现 `ListSkillsForPlatform` 和 `scanSkillsDirectory`
   - 实现 `readSkillMetadataExtended`
   - 实现 `ToggleSkill` 和 `patchSkillFrontMatterBool`
   - 实现 `GetSkillContent`、`SaveSkillContent`、`OpenSkillFolder`
   - 修改 `InstallSkill` 和 `UninstallSkillEx`

2. **前端 `skill.ts` API 更新**
   - 更新类型定义
   - 添加新 API 方法

3. **前端 `Skill/Index.vue` 重构**
   - 添加平台 Tab
   - 实现分组显示
   - 添加开关组件
   - 添加内容展开区
   - 添加安装模态框
   - 添加打开文件夹按钮

4. **国际化更新**
   - 添加中英文文案

5. **生成 Wails 绑定**
   - `wails3 task common:generate:bindings`

6. **Codex 核对验证**
   - 根据文档逐项验证功能
   - 检查是否引入新问题

---

## 9. 验收标准

- [ ] 平台 Tab 切换正常
- [ ] 技能分组显示正确（项目级 → 用户级 → 可用）
- [ ] 开关切换正确修改 `SKILL.md` 中的 `disable-model-invocation`
- [ ] 内容展开正确显示 SKILL.md
- [ ] 许可证徽章正确显示
- [ ] 安装模态框正确选择位置
- [ ] 打开文件夹正常工作
- [ ] 中英文切换正常
- [ ] 不影响现有技能管理功能
