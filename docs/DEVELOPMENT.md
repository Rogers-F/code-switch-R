# Development

本文件收纳开发/构建/发布相关信息（README 只保留功能与使用说明）。

## 环境要求

| 依赖 | 版本要求 | 安装命令 |
|------|---------|----------|
| Go | 1.24+ | https://golang.org/dl/ |
| Node.js | 18+ | https://nodejs.org/ |
| Wails 3 CLI | latest | `go install github.com/wailsapp/wails/v3/cmd/wails3@latest` |
| Task | latest | `go install github.com/go-task/task/v3/cmd/task@latest` |

### Linux 额外依赖

```bash
# Ubuntu/Debian
sudo apt-get install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev

# Fedora
sudo dnf install gtk3-devel webkit2gtk4.1-devel

# Arch Linux
sudo pacman -S base-devel webkit2gtk-4.1
```

## 本地开发

```bash
# 克隆项目
git clone https://github.com/SimonUTD/code-switch-R.git
cd code-switch-R

# 安装 Wails / Task（如未安装）
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
go install github.com/go-task/task/v3/cmd/task@latest

# 安装前端依赖
cd frontend
npm install
cd ..

# 开发运行
wails3 task dev
```

## 构建打包

### 基础构建

```bash
# 更新构建元数据
wails3 task common:update:build-assets

# 打包当前平台
wails3 task package

# Windows（示例）
task windows:build PRODUCTION=true
```

### Linux 平台打包

```bash
# 构建二进制
wails3 task linux:build

# 创建 AppImage
wails3 task linux:create:appimage

# 创建 DEB 包
wails3 task linux:create:deb

# 创建 RPM 包
wails3 task linux:create:rpm
```

### 交叉编译（示例：macOS）

```bash
# Windows
brew install mingw-w64
env ARCH=amd64 wails3 task windows:build
env ARCH=amd64 wails3 task windows:package

# Linux
env ARCH=amd64 wails3 task linux:build
```

## 发布流程

推送 tag 即可触发 GitHub Actions 自动构建：

```bash
git tag v1.2.0
git push origin v1.2.0
```

自动构建产物（以 workflow 配置为准）：
- macOS: `codeswitch-macos-arm64.zip`, `codeswitch-macos-amd64.zip`
- Windows: `CodeSwitch-amd64-installer.exe`, `CodeSwitch.exe`, `updater.exe`
- Linux: `CodeSwitch.AppImage`, `codeswitch_*.deb`, `codeswitch-*.rpm`

## 支持的发行版（参考）

| 发行版 | 版本 | 支持格式 | 推荐格式 |
|--------|------|----------|----------|
| Ubuntu | 24.04 LTS | DEB / AppImage | DEB |
| Ubuntu | 22.04 LTS | AppImage | AppImage |
| Debian | 12 (Bookworm) | DEB / AppImage | DEB |
| Fedora | 39/40 | RPM / AppImage | RPM |
| Linux Mint | 22+ | DEB / AppImage | DEB |
| Arch Linux | Rolling | AppImage | AppImage |
| openSUSE | Leap/Tumbleweed | AppImage | AppImage |

> 💡 Ubuntu 22.04 因 WebKit 版本限制（4.0），建议使用 AppImage。

## 常见问题

### 构建相关
- macOS 无法打开 `.app`：先执行 `wails3 task common:update:build-assets` 再构建
- macOS 交叉编译权限问题：终端可能需要完全磁盘访问权限
- Linux AppImage FUSE 问题：使用 `--appimage-extract-and-run` 参数运行

### 运行时问题
- 代理连接失败：检查端口 `18100` 是否被占用
- 供应商配置不生效：确认 CLI 配置文件中的端点指向 `localhost:18100`
- Gemini OAuth 失败：检查系统代理设置和网络连接

## 技术栈

| 组件 | 技术 | 版本 |
|------|------|------|
| 后端 | Go | 1.24+ |
| Web 框架 | Gin | latest |
| 数据库 | SQLite | 3.x |
| 前端 | Vue 3 | 3.x |
| 语言 | TypeScript | 5.x |
| 样式 | Tailwind CSS | 4.x（以 package.json 为准） |
| 桌面框架 | Wails 3 | 3.x |

