# Release / GitHub Actions

本项目已内置 GitHub Actions 自动打包与发布（见：`.github/workflows/release.yml`）。

## 一次性配置（在 GitHub 仓库页面）

1. **启用 Actions**：Settings → Actions → General，确保允许运行 workflow。
2. **给 GITHUB_TOKEN 写权限**：Settings → Actions → General → Workflow permissions → 选择 **Read and write permissions**。
3. （可选）如果你使用分支保护，确保 **允许 tag push**，否则无法触发发布 workflow。

## 推荐发布方式：推送 tag 自动发布

1. 在 `RELEASE_NOTES.md` 中新增一段版本说明（可选，但推荐）：
   - 标题格式：`# Code Switch vX.Y.Z`
2. 打 tag 并推送：

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

3. GitHub Actions 会自动执行：
   - macOS / Windows / Linux 构建与打包
   - 上传产物到 GitHub Release（含 `.sha256` 与 `latest.json`）

## 手动发布方式：workflow_dispatch

如果你需要对同一个 tag 重新打包/重新发布，可到 Actions → Release → Run workflow：
- `tag` 可选（例如 `vX.Y.Z`），**留空则从 `version_service.go` 读取 `AppVersion`**
- 可选设置 `draft` / `prerelease`

> 建议优先使用“推送 tag”触发；手动触发主要用于重跑。

## 发布产物（默认）

- macOS：`codeswitch-macos-arm64.zip` / `codeswitch-macos-amd64.zip`
- Windows：`CodeSwitch-amd64-installer.exe` / `CodeSwitch.exe` / `updater.exe`（含 `.sha256`）
- Linux：`CodeSwitch.AppImage` / `codeswitch_*.deb` / `codeswitch-*.rpm`（含 `.sha256`）
- 自动更新元数据：`latest.json`

## 自动更新（latest.json）

应用会优先从 GitHub Releases 下载 `latest.json`（URL 形式为：`.../releases/latest/download/latest.json`）。
如果你更换了仓库名或 owner，需要同步修改更新服务的默认仓库地址：
- `services/updateservice.go:182`
- `services/updateservice.go:2018`
