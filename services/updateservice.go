package services

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-version"
)

// UpdateInfo 更新信息
type UpdateInfo struct {
	Available    bool   `json:"available"`
	Version      string `json:"version"`
	DownloadURL  string `json:"download_url"`
	ReleaseNotes string `json:"release_notes"`
	FileSize     int64  `json:"file_size"`
	SHA256       string `json:"sha256"`
}

// UpdateState 更新状态
type UpdateState struct {
	LastCheckTime       time.Time `json:"last_check_time"`
	LastCheckSuccess    bool      `json:"last_check_success"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
	LatestKnownVersion  string    `json:"latest_known_version"`
	DownloadProgress    float64   `json:"download_progress"`
	UpdateReady         bool      `json:"update_ready"`
	AutoCheckEnabled    bool      `json:"auto_check_enabled"` // 新增：持久化自动检查开关
}

// UpdateService 更新服务
type UpdateService struct {
	currentVersion   string
	latestVersion    string
	downloadURL      string
	updateFilePath   string
	autoCheckEnabled bool
	downloadProgress float64
	dailyCheckTimer  *time.Timer
	lastCheckTime    time.Time
	checkFailures    int
	updateReady      bool
	isPortable       bool // 是否为便携版
	mu               sync.Mutex
	stateFile        string
	updateDir        string
	lockFile         string // 更新锁文件路径

	// 保存最新检查到的更新信息（含 SHA256）
	latestUpdateInfo *UpdateInfo
}

// GitHubRelease GitHub Release 结构
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// NewUpdateService 创建更新服务
func NewUpdateService(currentVersion string) *UpdateService {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	updateDir := filepath.Join(home, ".code-switch", "updates")
	stateFile := filepath.Join(home, ".code-switch", "update-state.json")

	us := &UpdateService{
		currentVersion:   currentVersion,
		autoCheckEnabled: true, // 默认开启自动检查
		isPortable:       detectPortableMode(),
		updateDir:        updateDir,
		stateFile:        stateFile,
	}

	// 创建更新目录
	_ = os.MkdirAll(updateDir, 0o755)

	// 加载状态（如果文件不存在，会保持默认值 true）
	_ = us.LoadState()

	log.Printf("[UpdateService] 运行模式: %s", func() string {
		if us.isPortable {
			return "便携版"
		}
		return "安装版"
	}())

	return us
}

// detectPortableMode 检测是否为便携版
// 采用写权限检测方式：如果能在 exe 所在目录创建文件，则为便携版
func detectPortableMode() bool {
	if runtime.GOOS != "windows" {
		return false // 非 Windows 默认不是便携版
	}

	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	exePath, _ = filepath.EvalSymlinks(exePath)
	exeDir := filepath.Dir(exePath)

	// 直接检测写权限（比路径匹配更准确）
	// 如果能在 exe 所在目录创建文件，则为便携版
	testFile := filepath.Join(exeDir, fmt.Sprintf(".write-test-%d", os.Getpid()))
	f, err := os.Create(testFile)
	if err != nil {
		// 无写权限，视为安装版（需要 UAC）
		log.Printf("[Update] 检测为安装版: 无法写入 %s", exeDir)
		return false
	}
	f.Close()
	os.Remove(testFile)

	log.Printf("[Update] 检测为便携版: 可写入 %s", exeDir)
	return true
}

// CheckUpdate 检查更新（带网络容错）
func (us *UpdateService) CheckUpdate() (*UpdateInfo, error) {
	log.Printf("[UpdateService] 开始检查更新，当前版本: %s", us.currentVersion)

	client := &http.Client{
		Timeout: 15 * time.Second, // 增加超时时间从10秒到15秒
	}

	releaseURL := "https://api.github.com/repos/SimonUTD/code-switch-R/releases/latest"

	req, err := http.NewRequest("GET", releaseURL, nil)
	if err != nil {
		log.Printf("[UpdateService] ❌ 创建请求失败: %v", err)
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CodeSwitch/"+us.currentVersion)

	log.Printf("[UpdateService] 请求 GitHub API: %s", releaseURL)

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[UpdateService] ❌ GitHub API 不可达: %v", err)
		return nil, fmt.Errorf("GitHub API 不可达: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[UpdateService] ❌ GitHub API 返回错误状态码: %d", resp.StatusCode)
		return nil, fmt.Errorf("GitHub API 返回错误状态码: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Printf("[UpdateService] ❌ 解析响应失败: %v", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	log.Printf("[UpdateService] 最新版本: %s", release.TagName)

	// 比较版本号
	needUpdate, err := us.compareVersions(us.currentVersion, release.TagName)
	if err != nil {
		log.Printf("[UpdateService] ❌ 版本比较失败: %v (current=%s, latest=%s)", err, us.currentVersion, release.TagName)
		return nil, fmt.Errorf("版本比较失败: %w", err)
	}

	if needUpdate {
		log.Printf("[UpdateService] ✅ 发现新版本: %s → %s", us.currentVersion, release.TagName)
	} else {
		log.Printf("[UpdateService] ✅ 已是最新版本: %s", us.currentVersion)
	}

	// 查找当前平台的下载链接
	downloadURL := us.findPlatformAsset(release.Assets)
	if downloadURL == "" {
		log.Printf("[UpdateService] ❌ 未找到适用于 %s 的安装包", runtime.GOOS)
		return nil, fmt.Errorf("未找到适用于 %s 的安装包", runtime.GOOS)
	}

	log.Printf("[UpdateService] 下载链接: %s", downloadURL)

	// 查找对应的 SHA256 校验文件
	sha256Hash := us.findSHA256ForAsset(release.Assets, downloadURL)
	if sha256Hash != "" {
		log.Printf("[UpdateService] SHA256: %s", sha256Hash)
	}

	updateInfo := &UpdateInfo{
		Available:    needUpdate,
		Version:      release.TagName,
		DownloadURL:  downloadURL,
		ReleaseNotes: release.Body,
		SHA256:       sha256Hash,
	}

	us.mu.Lock()
	us.latestVersion = release.TagName
	us.downloadURL = downloadURL
	us.latestUpdateInfo = updateInfo // 保存更新信息
	us.mu.Unlock()

	return updateInfo, nil
}

// compareVersions 比较版本号
func (us *UpdateService) compareVersions(current, latest string) (bool, error) {
	currentVer, err := version.NewVersion(current)
	if err != nil {
		return false, fmt.Errorf("解析当前版本失败: %w", err)
	}

	latestVer, err := version.NewVersion(latest)
	if err != nil {
		return false, fmt.Errorf("解析最新版本失败: %w", err)
	}

	return latestVer.GreaterThan(currentVer), nil
}

// findPlatformAsset 查找当前平台的下载链接
func (us *UpdateService) findPlatformAsset(assets []struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}) string {
	var targetName string
	switch runtime.GOOS {
	case "windows":
		// 统一下载核心 exe（无论便携版还是安装版）
		// 安装版通过 updater.exe 提权替换
		targetName = "CodeSwitch.exe"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			targetName = "codeswitch-macos-arm64.zip"
		} else {
			targetName = "codeswitch-macos-amd64.zip"
		}
	case "linux":
		targetName = "CodeSwitch.AppImage"
	default:
		return ""
	}

	// 精确匹配文件名
	for _, asset := range assets {
		if asset.Name == targetName {
			log.Printf("[UpdateService] 找到更新文件: %s (模式: %s)", targetName, func() string {
				if us.isPortable {
					return "便携版"
				}
				return "安装版"
			}())
			return asset.BrowserDownloadURL
		}
	}

	log.Printf("[UpdateService] 未找到适配文件 %s", targetName)
	return ""
}

// findSHA256ForAsset 查找资产对应的 SHA256 哈希
// SHA256 文件格式：<hash>  <filename> 或 <hash> <filename>
func (us *UpdateService) findSHA256ForAsset(assets []struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}, assetURL string) string {
	// 从 URL 提取文件名
	assetName := filepath.Base(assetURL)
	sha256FileName := assetName + ".sha256"

	// 查找 SHA256 文件
	var sha256URL string
	for _, asset := range assets {
		if asset.Name == sha256FileName {
			sha256URL = asset.BrowserDownloadURL
			break
		}
	}

	if sha256URL == "" {
		log.Printf("[UpdateService] 未找到 SHA256 文件: %s", sha256FileName)
		return ""
	}

	// 下载并解析 SHA256 文件
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(sha256URL)
	if err != nil {
		log.Printf("[UpdateService] 下载 SHA256 文件失败: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("[UpdateService] SHA256 文件返回错误状态码: %d", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[UpdateService] 读取 SHA256 文件失败: %v", err)
		return ""
	}

	// 解析格式：<hash>  <filename> 或 <hash> <filename>
	content := strings.TrimSpace(string(body))
	parts := strings.Fields(content)
	if len(parts) >= 1 {
		log.Printf("[UpdateService] 获取到 SHA256: %s", parts[0])
		return parts[0] // 返回哈希值
	}

	return ""
}

// DownloadUpdate 下载更新文件（支持更新锁、重试、断点续传、SHA256校验）
func (us *UpdateService) DownloadUpdate(progressCallback func(float64)) error {
	// 获取更新锁，防止并发下载
	if err := us.acquireUpdateLock(); err != nil {
		return err
	}
	defer us.releaseUpdateLock()

	us.mu.Lock()
	url := us.downloadURL
	expectedHash := ""
	if us.latestUpdateInfo != nil {
		expectedHash = us.latestUpdateInfo.SHA256
	}
	// 重置下载状态
	us.updateReady = false
	us.downloadProgress = 0
	us.mu.Unlock()
	us.SaveState()

	if url == "" {
		return fmt.Errorf("下载链接为空，请先检查更新")
	}

	filePath := filepath.Join(us.updateDir, filepath.Base(url))

	// 检查本地是否已有完整文件（断点续传场景：之前下载完成但未安装）
	if expectedHash != "" {
		if hash, err := calculateSHA256(filePath); err == nil && strings.EqualFold(hash, expectedHash) {
			log.Printf("[UpdateService] 本地已有完整文件，跳过下载")
			us.mu.Lock()
			us.updateFilePath = filePath
			us.downloadProgress = 100
			us.mu.Unlock()
			return us.PrepareUpdate()
		}
	}

	// 三次重试下载
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		if err := us.downloadWithResume(url, filePath, progressCallback); err != nil {
			lastErr = err
			log.Printf("[UpdateService] 下载失败（第%d次）: %v", attempt, err)
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			continue
		}
		lastErr = nil
		break
	}
	if lastErr != nil {
		_ = os.Remove(filePath) // 清理残留文件
		return fmt.Errorf("下载失败: %w", lastErr)
	}

	// SHA256 校验
	if expectedHash != "" {
		if err := us.verifyDownload(filePath, expectedHash); err != nil {
			_ = os.Remove(filePath)
			return err
		}
	}

	us.mu.Lock()
	us.updateFilePath = filePath
	us.downloadProgress = 100
	us.mu.Unlock()

	// 下载成功后立即准备更新，写入 pending 标记并持久化 SHA256
	if err := us.PrepareUpdate(); err != nil {
		return fmt.Errorf("准备更新失败: %w", err)
	}

	return nil
}

// downloadWithResume 支持断点续传的下载
func (us *UpdateService) downloadWithResume(url, dest string, progressCallback func(float64)) error {
	client := &http.Client{Timeout: 5 * time.Minute}

	var start int64
	var total int64
	if info, err := os.Stat(dest); err == nil {
		start = info.Size()
	}

	// HEAD 请求检查是否支持 Range
	if start > 0 {
		if head, err := client.Head(url); err == nil && head.StatusCode == http.StatusOK {
			if strings.EqualFold(head.Header.Get("Accept-Ranges"), "bytes") {
				total = head.ContentLength
				log.Printf("[UpdateService] 断点续传: 从 %d 字节继续下载", start)
			} else {
				start = 0
				_ = os.Remove(dest)
			}
		} else {
			start = 0
			_ = os.Remove(dest)
		}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("下载失败，HTTP 状态码: %d", resp.StatusCode)
	}

	if total == 0 {
		total = resp.ContentLength
		if total > 0 && start > 0 {
			total += start
		}
	}

	var out *os.File
	if start > 0 {
		out, err = os.OpenFile(dest, os.O_WRONLY|os.O_APPEND, 0o644)
	} else {
		out, err = os.Create(dest)
	}
	if err != nil {
		return err
	}
	defer out.Close()

	downloaded := start
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := out.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("写入文件失败: %w", writeErr)
			}
			downloaded += int64(n)

			if total > 0 && progressCallback != nil {
				progress := float64(downloaded) / float64(total) * 100
				us.mu.Lock()
				us.downloadProgress = progress
				us.mu.Unlock()
				progressCallback(progress)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("读取数据失败: %w", readErr)
		}
	}
	return nil
}

// PrepareUpdate 准备更新
func (us *UpdateService) PrepareUpdate() error {
	us.mu.Lock()

	if us.updateFilePath == "" {
		us.mu.Unlock()
		return fmt.Errorf("更新文件路径为空")
	}

	// 写入待更新标记（包含 SHA256 用于重启后校验）
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	metadata := map[string]interface{}{
		"version":       us.latestVersion,
		"download_path": us.updateFilePath,
		"download_time": time.Now().Format(time.RFC3339),
	}

	// 持久化 SHA256（关键：重启后 latestUpdateInfo 会丢失）
	if us.latestUpdateInfo != nil && us.latestUpdateInfo.SHA256 != "" {
		metadata["sha256"] = us.latestUpdateInfo.SHA256
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		us.mu.Unlock()
		return fmt.Errorf("序列化元数据失败: %w", err)
	}

	if err := os.WriteFile(pendingFile, data, 0o644); err != nil {
		us.mu.Unlock()
		return fmt.Errorf("写入标记文件失败: %w", err)
	}

	us.updateReady = true
	us.mu.Unlock() // 释放锁后再调用 SaveState，避免死锁

	us.SaveState()

	return nil
}

// ApplyUpdate 应用更新（启动时调用）
// 添加更新锁防止并发，SHA256 校验防止损坏文件
func (us *UpdateService) ApplyUpdate() error {
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")

	// 检查是否有待更新
	if _, err := os.Stat(pendingFile); os.IsNotExist(err) {
		return nil // 没有待更新
	}

	// 获取更新锁
	if err := us.acquireUpdateLock(); err != nil {
		log.Printf("[UpdateService] 获取更新锁失败，跳过更新: %v", err)
		return nil // 另一个更新正在进行，静默跳过
	}
	defer us.releaseUpdateLock()

	// 读取元数据
	data, err := os.ReadFile(pendingFile)
	if err != nil {
		us.clearPendingState()
		return fmt.Errorf("读取标记文件失败: %w", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(data, &metadata); err != nil {
		us.clearPendingState()
		return fmt.Errorf("解析元数据失败: %w", err)
	}

	downloadPath, ok := metadata["download_path"].(string)
	if !ok || downloadPath == "" {
		us.clearPendingState()
		return fmt.Errorf("元数据中缺少下载路径")
	}

	// 检查下载文件是否存在
	if _, err := os.Stat(downloadPath); os.IsNotExist(err) {
		us.clearPendingState()
		return fmt.Errorf("更新文件不存在: %s", downloadPath)
	}

	// 从元数据恢复 SHA256 并验证
	var expectedHash string
	if sha256Hash, ok := metadata["sha256"].(string); ok && sha256Hash != "" {
		expectedHash = sha256Hash
		us.mu.Lock()
		us.latestUpdateInfo = &UpdateInfo{
			SHA256: sha256Hash,
		}
		us.mu.Unlock()
		log.Printf("[UpdateService] 从元数据恢复 SHA256: %s", sha256Hash)
	}

	// SHA256 校验（如果有）
	if expectedHash != "" {
		if err := us.verifyDownload(downloadPath, expectedHash); err != nil {
			log.Printf("[UpdateService] SHA256 校验失败: %v", err)
			us.clearPendingState()
			_ = os.Remove(downloadPath) // 删除损坏的文件
			return fmt.Errorf("更新文件校验失败: %w", err)
		}
		log.Println("[UpdateService] SHA256 校验通过")
	}

	// 根据平台执行安装
	var installErr error
	switch runtime.GOOS {
	case "windows":
		installErr = us.applyUpdateWindows(downloadPath)
	case "darwin":
		installErr = us.applyUpdateDarwin(downloadPath)
	case "linux":
		installErr = us.applyUpdateLinux(downloadPath)
	default:
		installErr = fmt.Errorf("不支持的平台: %s", runtime.GOOS)
	}

	if installErr != nil {
		// 安装失败，清理状态但保留下载文件（可能需要重试）
		us.clearPendingState()
		return installErr
	}

	// 清理标记文件（成功情况下由平台特定函数清理）
	return nil
}

// clearPendingState 统一清理更新状态（成功或失败后调用）
func (us *UpdateService) clearPendingState() {
	pendingFile := filepath.Join(filepath.Dir(us.stateFile), ".pending-update")
	_ = os.Remove(pendingFile)

	us.mu.Lock()
	us.updateReady = false
	us.downloadProgress = 0
	us.mu.Unlock()

	us.SaveState()
	log.Println("[UpdateService] 已清理更新状态")
}

// applyUpdateWindows Windows 平台更新
func (us *UpdateService) applyUpdateWindows(updatePath string) error {
	if us.isPortable {
		// 便携版：替换当前可执行文件
		return us.applyPortableUpdate(updatePath)
	}

	// 安装版：使用 updater.exe 辅助程序静默更新
	return us.applyInstalledUpdate(updatePath)
}

// applyPortableUpdate 便携版更新逻辑
// 使用 PowerShell 脚本等待当前进程退出后替换文件，解决 Windows 文件锁定问题
func (us *UpdateService) applyPortableUpdate(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}

	// 解析符号链接（如果有）
	currentExe, err = filepath.EvalSymlinks(currentExe)
	if err != nil {
		return fmt.Errorf("解析符号链接失败: %w", err)
	}

	log.Printf("[UpdateService] 便携版更新: %s -> %s", newExePath, currentExe)

	// 清理更新状态
	us.clearPendingState()

	// 构建 PowerShell 脚本：等待进程退出 → 替换文件 → 启动新版本
	backupPath := currentExe + ".old"
	pid := os.Getpid()

	// PowerShell 脚本内容
	psScript := fmt.Sprintf(`
$ErrorActionPreference = 'Stop'
$pid = %d
$currentExe = '%s'
$newExe = '%s'
$backupPath = '%s'

# 等待主进程退出（最多 30 秒）
$proc = Get-Process -Id $pid -ErrorAction SilentlyContinue
if ($proc) {
    Write-Host "等待进程 $pid 退出..."
    $proc.WaitForExit(30000) | Out-Null
}

# 短暂延迟确保文件释放
Start-Sleep -Milliseconds 500

# 备份旧文件
if (Test-Path $currentExe) {
    Move-Item -Path $currentExe -Destination $backupPath -Force
    Write-Host "已备份: $backupPath"
}

# 复制新文件
Copy-Item -Path $newExe -Destination $currentExe -Force
Write-Host "已替换: $currentExe"

# 清理备份（延迟删除）
Start-Sleep -Seconds 2
if (Test-Path $backupPath) {
    Remove-Item -Path $backupPath -Force -ErrorAction SilentlyContinue
}

# 启动新版本
Start-Process -FilePath $currentExe
Write-Host "更新完成，已启动新版本"
`,
		pid,
		strings.ReplaceAll(currentExe, `\`, `\\`),
		strings.ReplaceAll(newExePath, `\`, `\\`),
		strings.ReplaceAll(backupPath, `\`, `\\`),
	)

	// 将脚本写入临时文件
	scriptPath := filepath.Join(us.updateDir, "update-portable.ps1")
	if err := os.WriteFile(scriptPath, []byte(psScript), 0o644); err != nil {
		return fmt.Errorf("写入更新脚本失败: %w", err)
	}

	log.Printf("[UpdateService] 已创建更新脚本: %s", scriptPath)

	// 启动 PowerShell 执行脚本（-WindowStyle Hidden 隐藏窗口）
	cmd := exec.Command("powershell.exe",
		"-ExecutionPolicy", "Bypass",
		"-WindowStyle", "Hidden",
		"-File", scriptPath,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动更新脚本失败: %w", err)
	}

	log.Printf("[UpdateService] 更新脚本已启动 (PID=%d)，准备退出主程序...", cmd.Process.Pid)

	// 释放更新锁
	us.releaseUpdateLock()

	// 退出当前进程，让 PowerShell 脚本完成替换
	os.Exit(0)
	return nil
}

// applyUpdateDarwin macOS 平台更新
func (us *UpdateService) applyUpdateDarwin(zipPath string) error {
	// TODO: 实现 macOS 更新逻辑
	// 1. 解压 zip 文件
	// 2. 替换 /Applications/CodeSwitch.app
	// 3. 重启应用
	log.Println("[UpdateService] macOS 更新功能待实现")
	return nil
}

// applyUpdateLinux Linux 平台更新（增强版）
func (us *UpdateService) applyUpdateLinux(appImagePath string) error {
	// 1. SHA256 校验
	us.mu.Lock()
	var expectedHash string
	if us.latestUpdateInfo != nil {
		expectedHash = us.latestUpdateInfo.SHA256
	}
	us.mu.Unlock()

	if expectedHash != "" {
		actualHash, err := calculateSHA256(appImagePath)
		if err != nil {
			return fmt.Errorf("计算 SHA256 失败: %w", err)
		}
		if !strings.EqualFold(actualHash, expectedHash) {
			return fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", expectedHash, actualHash)
		}
		log.Println("[UpdateService] SHA256 校验通过")
	}

	// 2. ELF 格式校验
	f, err := os.Open(appImagePath)
	if err != nil {
		return fmt.Errorf("无法打开 AppImage: %w", err)
	}
	magic := make([]byte, 4)
	_, err = f.Read(magic)
	f.Close()
	if err != nil || magic[0] != 0x7F || magic[1] != 'E' || magic[2] != 'L' || magic[3] != 'F' {
		return fmt.Errorf("无效的 AppImage 格式（非 ELF）")
	}

	// 3. 获取当前可执行文件路径
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}
	currentExe, _ = filepath.EvalSymlinks(currentExe)

	// 4. 带时间戳的备份（保留最近 2 个）
	timestamp := time.Now().Format("20060102-150405")
	backupPath := currentExe + ".backup-" + timestamp
	if err := copyUpdateFile(currentExe, backupPath); err != nil {
		log.Printf("[UpdateService] 备份失败（继续）: %v", err)
	}

	// 5. 替换可执行文件
	if err := copyUpdateFile(appImagePath, currentExe); err != nil {
		// 尝试恢复
		_ = copyUpdateFile(backupPath, currentExe)
		return fmt.Errorf("替换失败: %w", err)
	}

	// 6. 设置可执行权限
	if err := os.Chmod(currentExe, 0o755); err != nil {
		return fmt.Errorf("设置执行权限失败: %w", err)
	}

	// 7. 清理旧备份（保留最近 2 个）
	us.cleanupOldBackups(filepath.Dir(currentExe), "*.backup-*", 2)

	log.Println("[UpdateService] Linux 更新应用成功")
	return nil
}

// cleanupOldBackups 清理旧备份文件，保留最近 n 个
func (us *UpdateService) cleanupOldBackups(dir, pattern string, keep int) {
	matches, _ := filepath.Glob(filepath.Join(dir, pattern))
	if len(matches) <= keep {
		return
	}

	// 按修改时间排序（新 → 旧）
	sort.Slice(matches, func(i, j int) bool {
		fi, _ := os.Stat(matches[i])
		fj, _ := os.Stat(matches[j])
		if fi == nil || fj == nil {
			return false
		}
		return fi.ModTime().After(fj.ModTime())
	})

	// 删除旧的
	for _, f := range matches[keep:] {
		os.Remove(f)
		log.Printf("[UpdateService] 清理旧备份: %s", f)
	}
}

// RestartApp 重启应用
// 如果有待安装的更新，会先触发更新流程（Windows 安装版会请求 UAC）
func (us *UpdateService) RestartApp() error {
	// 有待安装的更新时直接触发安装（Windows 安装版会请求 UAC）
	if err := us.ApplyUpdate(); err != nil {
		log.Printf("[UpdateService] 应用更新失败，将执行普通重启: %v", err)
	}

	// ApplyUpdate 在成功安装更新时会退出进程；走到这里说明没有待安装任务或更新失败
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}

	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command(executable)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动新进程失败: %w", err)
		}
		os.Exit(0)

	case "darwin":
		cmd := exec.Command("open", "-n", executable)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动新进程失败: %w", err)
		}
		os.Exit(0)

	case "linux":
		cmd := exec.Command(executable)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动新进程失败: %w", err)
		}
		os.Exit(0)
	}

	return nil
}

// StartDailyCheck 启动每日8点定时检查
func (us *UpdateService) StartDailyCheck() {
	us.stopDailyCheck()

	duration := us.calculateNextCheckDuration()
	us.dailyCheckTimer = time.AfterFunc(duration, func() {
		us.performDailyCheck()
		us.StartDailyCheck() // 重新调度下次检查
	})

	log.Printf("[UpdateService] 定时检查已启动，下次检查时间: %s", time.Now().Add(duration).Format("2006-01-02 15:04:05"))
}

// stopDailyCheck 停止定时检查
func (us *UpdateService) stopDailyCheck() {
	if us.dailyCheckTimer != nil {
		us.dailyCheckTimer.Stop()
		us.dailyCheckTimer = nil
	}
}

// calculateNextCheckDuration 计算距离下一个8点的时长
func (us *UpdateService) calculateNextCheckDuration() time.Duration {
	now := time.Now()

	// 今天早上8点
	next := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, now.Location())

	// 如果已经过了今天8点，调整到明天8点
	if now.After(next) {
		next = next.Add(24 * time.Hour)
	}

	return next.Sub(now)
}

// performDailyCheck 执行每日检查（带重试）
func (us *UpdateService) performDailyCheck() {
	log.Println("[UpdateService] 开始每日定时检查更新...")

	var updateInfo *UpdateInfo
	var err error

	// 重试机制：最多3次，间隔5分钟
	for i := 0; i < 3; i++ {
		updateInfo, err = us.CheckUpdate()

		if err == nil {
			// 检查成功
			us.mu.Lock()
			us.lastCheckTime = time.Now()
			us.checkFailures = 0
			us.mu.Unlock()
			us.SaveState()

			if updateInfo.Available {
				log.Printf("[UpdateService] 发现新版本 %s，开始下载...", updateInfo.Version)
				go us.autoDownload()
			} else {
				log.Println("[UpdateService] 已是最新版本")
			}
			return
		}

		// 网络错误，记录日志
		log.Printf("[UpdateService] 检查更新失败（第%d次）: %v", i+1, err)

		us.mu.Lock()
		us.checkFailures++
		us.mu.Unlock()

		if i < 2 { // 不是最后一次，等待后重试
			time.Sleep(5 * time.Minute)
		}
	}

	// 3次都失败，静默放弃
	us.SaveState()
	log.Println("[UpdateService] 检查更新失败，将在明天8点重试")
}

// autoDownload 自动下载更新（静默失败）
func (us *UpdateService) autoDownload() {
	err := us.DownloadUpdate(func(progress float64) {
		log.Printf("[UpdateService] 下载进度: %.2f%%", progress)
	})

	if err != nil {
		log.Printf("[UpdateService] 自动下载失败: %v", err)
		return
	}

	// DownloadUpdate 内部已调用 PrepareUpdate，无需重复调用
	log.Println("[UpdateService] 更新已下载完成，等待用户重启应用")
}

// CheckUpdateAsync 异步检查更新
func (us *UpdateService) CheckUpdateAsync() {
	go func() {
		updateInfo, err := us.CheckUpdate()
		if err != nil {
			log.Printf("[UpdateService] 检查更新失败: %v", err)
			us.mu.Lock()
			us.checkFailures++
			us.mu.Unlock()
			us.SaveState()
			return
		}

		us.mu.Lock()
		us.lastCheckTime = time.Now()
		us.checkFailures = 0
		us.mu.Unlock()
		us.SaveState()

		if updateInfo.Available {
			log.Printf("[UpdateService] 发现新版本 %s", updateInfo.Version)
			go us.autoDownload()
		}
	}()
}

// GetUpdateState 获取更新状态
func (us *UpdateService) GetUpdateState() *UpdateState {
	us.mu.Lock()
	defer us.mu.Unlock()

	return &UpdateState{
		LastCheckTime:       us.lastCheckTime,
		LastCheckSuccess:    us.checkFailures == 0,
		ConsecutiveFailures: us.checkFailures,
		LatestKnownVersion:  us.latestVersion,
		DownloadProgress:    us.downloadProgress,
		UpdateReady:         us.updateReady,
		AutoCheckEnabled:    us.autoCheckEnabled, // 返回自动检查状态
	}
}

// IsAutoCheckEnabled 是否启用自动检查
func (us *UpdateService) IsAutoCheckEnabled() bool {
	us.mu.Lock()
	defer us.mu.Unlock()
	return us.autoCheckEnabled
}

// SetAutoCheckEnabled 设置是否启用自动检查
func (us *UpdateService) SetAutoCheckEnabled(enabled bool) {
	us.mu.Lock()
	us.autoCheckEnabled = enabled
	us.mu.Unlock()

	if enabled {
		us.StartDailyCheck()
	} else {
		us.stopDailyCheck()
	}

	us.SaveState()
}

// SaveState 保存状态
func (us *UpdateService) SaveState() error {
	us.mu.Lock()
	defer us.mu.Unlock()

	state := UpdateState{
		LastCheckTime:       us.lastCheckTime,
		LastCheckSuccess:    us.checkFailures == 0,
		ConsecutiveFailures: us.checkFailures,
		LatestKnownVersion:  us.latestVersion,
		DownloadProgress:    us.downloadProgress,
		UpdateReady:         us.updateReady,
		AutoCheckEnabled:    us.autoCheckEnabled, // 持久化自动检查开关
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化状态失败: %w", err)
	}

	dir := filepath.Dir(us.stateFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	return os.WriteFile(us.stateFile, data, 0o644)
}

// LoadState 加载状态
func (us *UpdateService) LoadState() error {
	data, err := os.ReadFile(us.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在，保存默认配置
			_ = us.SaveState()
			return nil
		}
		return fmt.Errorf("读取状态文件失败: %w", err)
	}

	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("解析状态失败: %w", err)
	}

	us.mu.Lock()
	us.lastCheckTime = state.LastCheckTime
	us.checkFailures = state.ConsecutiveFailures
	us.latestVersion = state.LatestKnownVersion
	us.downloadProgress = state.DownloadProgress
	us.updateReady = state.UpdateReady

	// 检查文件中是否包含 auto_check_enabled 字段
	// 如果包含，使用文件中的值；否则保持默认值 true（兼容老版本）
	dataStr := string(data)
	if strings.Contains(dataStr, "\"auto_check_enabled\"") {
		// 文件中包含 auto_check_enabled 字段，使用文件中的值
		us.autoCheckEnabled = state.AutoCheckEnabled
	}
	// 否则保持初始化时设置的默认值 true
	us.mu.Unlock()

	return nil
}

// copyUpdateFile 复制更新文件
func copyUpdateFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

// calculateSHA256 计算文件 SHA256
func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// ============================================================
// 以下为 Windows 安装版静默更新相关方法
// ============================================================

// acquireUpdateLock 获取更新锁（防止并发更新）
func (us *UpdateService) acquireUpdateLock() error {
	lockPath := filepath.Join(us.updateDir, "update.lock")

	// 尝试创建锁文件（排他模式）
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			// 检查锁文件是否过期（超过 10 分钟视为死锁）
			info, statErr := os.Stat(lockPath)
			if statErr == nil && time.Since(info.ModTime()) > 10*time.Minute {
				log.Printf("[UpdateService] 检测到过期锁文件，强制删除: %s", lockPath)
				os.Remove(lockPath)
				return us.acquireUpdateLock() // 重试
			}
			return fmt.Errorf("另一个更新正在进行中")
		}
		return fmt.Errorf("创建锁文件失败: %w", err)
	}

	// 写入 PID 和时间戳
	fmt.Fprintf(f, "%d\n%s", os.Getpid(), time.Now().Format(time.RFC3339))
	f.Close()

	us.lockFile = lockPath
	log.Printf("[UpdateService] 已获取更新锁: %s", lockPath)
	return nil
}

// releaseUpdateLock 释放更新锁
func (us *UpdateService) releaseUpdateLock() {
	if us.lockFile != "" {
		if err := os.Remove(us.lockFile); err != nil {
			log.Printf("[UpdateService] 释放锁文件失败: %v", err)
		} else {
			log.Printf("[UpdateService] 已释放更新锁: %s", us.lockFile)
		}
		us.lockFile = ""
	}
}

// downloadAndVerify 下载文件并验证 SHA256
func (us *UpdateService) downloadAndVerify(assetName string) (string, error) {
	releaseBaseURL := "https://github.com/SimonUTD/code-switch-R/releases/download"

	// 1. 下载主文件
	mainURL := fmt.Sprintf("%s/%s/%s", releaseBaseURL, us.latestVersion, assetName)
	mainPath := filepath.Join(us.updateDir, assetName)

	log.Printf("[UpdateService] 下载文件: %s", mainURL)
	if err := us.downloadFile(mainURL, mainPath); err != nil {
		return "", fmt.Errorf("下载 %s 失败: %w", assetName, err)
	}

	// 2. 下载哈希文件
	hashURL := mainURL + ".sha256"
	hashPath := mainPath + ".sha256"

	log.Printf("[UpdateService] 下载哈希文件: %s", hashURL)
	if err := us.downloadFile(hashURL, hashPath); err != nil {
		os.Remove(mainPath) // 清理已下载的主文件
		return "", fmt.Errorf("下载哈希文件失败: %w", err)
	}

	// 3. 解析哈希文件（格式: "hash  filename"）
	hashContent, err := os.ReadFile(hashPath)
	if err != nil {
		os.Remove(mainPath)
		os.Remove(hashPath)
		return "", fmt.Errorf("读取哈希文件失败: %w", err)
	}

	fields := strings.Fields(string(hashContent))
	if len(fields) == 0 {
		os.Remove(mainPath)
		os.Remove(hashPath)
		return "", fmt.Errorf("哈希文件格式错误")
	}
	expectedHash := fields[0]
	os.Remove(hashPath) // 哈希文件用完即删

	// 4. 校验主文件
	if err := us.verifyDownload(mainPath, expectedHash); err != nil {
		os.Remove(mainPath)
		return "", err
	}

	log.Printf("[UpdateService] 文件校验通过: %s", mainPath)
	return mainPath, nil
}

// downloadFile 下载单个文件
func (us *UpdateService) downloadFile(url, destPath string) error {
	client := &http.Client{
		Timeout: 5 * time.Minute, // 大文件下载超时
	}

	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

// verifyDownload 验证下载文件的 SHA256
func (us *UpdateService) verifyDownload(filePath, expectedHash string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("计算哈希失败: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))

	if !strings.EqualFold(actual, expectedHash) {
		return fmt.Errorf("SHA256 校验失败: 期望 %s, 实际 %s", expectedHash, actual)
	}

	log.Printf("[UpdateService] SHA256 校验通过: %s", filePath)
	return nil
}

// downloadUpdater 从 GitHub Release 下载 updater.exe
func (us *UpdateService) downloadUpdater(targetPath string) error {
	// 尝试下载带 SHA256 校验的 updater.exe
	updaterPath, err := us.downloadAndVerify("updater.exe")
	if err != nil {
		log.Printf("[UpdateService] 下载 updater.exe（带校验）失败: %v，尝试直接下载", err)

		// 降级：直接下载（不校验）
		url := fmt.Sprintf("https://github.com/SimonUTD/code-switch-R/releases/download/%s/updater.exe", us.latestVersion)
		log.Printf("[UpdateService] 直接下载更新器: %s", url)

		if err := us.downloadFile(url, targetPath); err != nil {
			return fmt.Errorf("下载更新器失败: %w", err)
		}
		return nil
	}

	// 如果下载路径不同，移动文件
	if updaterPath != targetPath {
		if err := os.Rename(updaterPath, targetPath); err != nil {
			// 重命名失败，尝试复制
			if err := copyUpdateFile(updaterPath, targetPath); err != nil {
				return fmt.Errorf("移动 updater.exe 失败: %w", err)
			}
			os.Remove(updaterPath)
		}
	}

	return nil
}

// calculateTimeout 根据文件大小动态计算超时时间
func calculateTimeout(fileSize int64) int {
	base := 30 // 基础 30 秒
	// 每 100MB 增加 10 秒
	extra := int(fileSize/(100*1024*1024)) * 10
	return base + extra
}

// applyInstalledUpdate 安装版更新逻辑（使用 PowerShell UAC 提权）
// 通过 PowerShell 的 Start-Process -Verb RunAs 触发 UAC 弹窗
func (us *UpdateService) applyInstalledUpdate(newExePath string) error {
	currentExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
	}
	currentExe, _ = filepath.EvalSymlinks(currentExe)

	// 1. 获取或下载 updater.exe
	updaterPath := filepath.Join(us.updateDir, "updater.exe")
	if _, err := os.Stat(updaterPath); os.IsNotExist(err) {
		log.Printf("[UpdateService] updater.exe 不存在，开始下载...")
		if err := us.downloadUpdater(updaterPath); err != nil {
			return fmt.Errorf("下载更新器失败: %w", err)
		}
	}

	// 2. 计算超时时间
	fileInfo, err := os.Stat(newExePath)
	if err != nil {
		return fmt.Errorf("获取新版本文件信息失败: %w", err)
	}
	timeout := calculateTimeout(fileInfo.Size())

	// 3. 创建更新任务配置
	taskFile := filepath.Join(us.updateDir, "update-task.json")
	task := map[string]interface{}{
		"main_pid":     os.Getpid(),
		"target_exe":   currentExe,
		"new_exe_path": newExePath,
		"backup_path":  currentExe + ".old",
		"cleanup_paths": []string{
			newExePath,
			filepath.Join(filepath.Dir(us.stateFile), ".pending-update"),
		},
		"timeout_sec": timeout,
	}

	taskData, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化任务配置失败: %w", err)
	}

	if err := os.WriteFile(taskFile, taskData, 0o644); err != nil {
		return fmt.Errorf("写入任务配置失败: %w", err)
	}

	log.Printf("[UpdateService] 已创建更新任务: %s", taskFile)
	log.Printf("[UpdateService] 任务配置: PID=%d, Timeout=%ds", os.Getpid(), timeout)

	// 4. 清理更新状态
	us.clearPendingState()

	// 5. 使用 PowerShell 以管理员权限启动 updater.exe
	// Start-Process -Verb RunAs 会触发 UAC 弹窗
	log.Printf("[UpdateService] 使用 UAC 提权启动更新器: %s", updaterPath)
	cmd := exec.Command("powershell.exe",
		"-ExecutionPolicy", "Bypass",
		"-Command",
		fmt.Sprintf(`Start-Process -FilePath '%s' -ArgumentList '%s' -Verb RunAs -WindowStyle Hidden`,
			strings.ReplaceAll(updaterPath, `'`, `''`),
			strings.ReplaceAll(taskFile, `'`, `''`),
		),
	)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 UAC 提权更新器失败: %w", err)
	}

	log.Printf("[UpdateService] UAC 提权请求已发送，准备退出主程序...")

	// 6. 释放更新锁
	us.releaseUpdateLock()

	// 7. 退出主程序
	os.Exit(0)
	return nil
}
