package main

import (
	"codeswitch/services"
	"embed"
	_ "embed"
	"fmt"
	"log"
	"log/slog"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daodao97/xgo/xlog"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/dock"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

//go:embed assets/icon.png assets/icon-dark.png
var trayIcons embed.FS

type AppService struct {
	App        *application.App
	TrayWindow application.Window
	// showMain 由 main() 注入带守卫的"显示并聚焦主窗口"闭包。
	// 前端（托盘弹窗）必须经这里聚焦主窗口，不能直接调 runtime 的
	// Window.Focus——alpha.38 的 Focus 没有 impl 判空，绕过守卫就绕回了闪退
	showMain func(withFocus bool)
}

// FocusMainWindow 聚焦主窗口（统一守卫入口，供前端 RPC 调用）
func (a *AppService) FocusMainWindow() {
	if a.showMain != nil {
		a.showMain(true)
	}
}

// setApp 故意不导出：AppService 注册为 Wails 服务后，导出方法一律可被前端 RPC 调用，
// 导出成 SetApp 等于让前端能把 App 引用置为 nil，之后所有开窗动作全部失效。
func (a *AppService) setApp(app *application.App) {
	a.App = app
}

func (a *AppService) SetTrayWindowHeight(height int) {
	if runtime.GOOS != "darwin" || a.TrayWindow == nil {
		return
	}
	if height < trayWindowMinHeight {
		height = trayWindowMinHeight
	}
	if height > trayWindowMaxHeight {
		height = trayWindowMaxHeight
	}
	a.TrayWindow.SetSize(trayWindowWidth, height)
}

func (a *AppService) OpenSecondWindow() {
	if a.App == nil {
		fmt.Println("[ERROR] app not initialized")
		return
	}
	name := fmt.Sprintf("logs-%d", time.Now().UnixNano())
	win := a.App.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Logs",
		Name:      name,
		Width:     1024,
		Height:    800,
		MinWidth:  600,
		MinHeight: 300,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			TitleBar:                application.MacTitleBarHidden,
			Backdrop:                application.MacBackdropTransparent,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/#/logs",
	})
	win.Center()
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {
	appservice := &AppService{}
	// shuttingDown：退出流程一旦开始，托盘点击等原生回调不该再触碰窗口——
	// Wails alpha.38 的 WebviewWindow.Focus() 没有 impl 判空，窗口销毁后调用
	// 会在 Win32 消息循环线程上 nil 解引用直接杀进程。
	// appStarted：Run() 之前窗口 impl 尚未创建，同一批方法在启动早期同样会炸
	var shuttingDown atomic.Bool
	var appStarted atomic.Bool

	// xgo 的 xlog 默认 Debug 级别：xrequest 会把每个转发请求的完整 curl（含整个
	// 请求体）作为 Debug 日志格式化输出，大上下文会话下每请求多出数十 MB 的
	// 字符串处理。收敛到 Warn 只保留网络错误类告警。必须放在任何 xgo 组件与
	// NewConsoleService 之前：xlog 的 handler 在创建时捕获当时的 os.Stdout
	xlog.SetLogger(xlog.StdoutTextPretty(xlog.WithLevel(slog.LevelWarn)))

	// 【修复】第一步：初始化数据库（必须最先执行）
	// 解决问题：InitGlobalDBQueue 依赖 xdb.DB("default")，但 xdb.Inits() 在 NewProviderRelayService 中
	if err := services.InitDatabase(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	log.Println("✅ 数据库已初始化")

	// 【修复】第二步：初始化写入队列（依赖数据库连接）
	if err := services.InitGlobalDBQueue(); err != nil {
		log.Fatalf("初始化数据库队列失败: %v", err)
	}
	log.Println("✅ 数据库写入队列已启动")

	// 【修复】第三步：创建服务（现在可以安全使用数据库了）
	// SuiStore 只服务快捷键存储，初始化失败不应该阻断整个应用启动：
	// 记录告警并跳过注册，其余功能照常可用
	suiService, errt := services.NewSuiStore()
	if errt != nil {
		log.Printf("⚠️ SuiStore 初始化失败，快捷键存储功能不可用: %v", errt)
		suiService = nil
	}

	providerService := services.NewProviderService()
	settingsService := services.NewSettingsService()
	autoStartService := services.NewAutoStartService()
	appSettings := services.NewAppSettingsService(autoStartService)
	notificationService := services.NewNotificationService(appSettings) // 通知服务
	// 模型元数据同步:载入本地缓存/内置种子并完成首次定价快照重建,
	// 默认模型策略从同步目录动态解析
	defaultModelPolicy := services.NewDefaultModelPolicy()
	modelSyncService := services.NewModelSyncService(appSettings, defaultModelPolicy)
	blacklistService := services.NewBlacklistService(settingsService, notificationService)
	// 监听地址取自用户的网络设置，默认仅回环。
	// 原先写死 ":18100" 会绑到全部网卡，把带供应商 API Key 的代理暴露给整个局域网，
	// 且让设置页的 localhost 模式形同虚设。
	relayListenAddrs := services.ResolveRelayListenAddresses()
	for _, addr := range relayListenAddrs {
		if addr != "127.0.0.1:18100" {
			log.Printf("⚠️ 代理按网络设置监听 %s（非仅回环，该网段内的其它设备可访问，请确认这是你需要的）", addr)
		}
	}
	// 写进各 CLI 配置的必须是"连接地址"而不是"绑定地址"：
	// lan 模式绑的是 0.0.0.0，把它写成 base_url 客户端根本连不上
	relayConnectAddr := services.RelayConnectAddress(relayListenAddrs[0])

	geminiService := services.NewGeminiService(relayConnectAddr, defaultModelPolicy)
	providerRelay := services.NewProviderRelayService(providerService, geminiService, blacklistService, notificationService, appSettings, relayListenAddrs...)
	claudeSettings := services.NewClaudeSettingsService(relayConnectAddr)
	codexSettings := services.NewCodexSettingsService(relayConnectAddr, defaultModelPolicy)
	cliConfigService := services.NewCliConfigService(relayConnectAddr, defaultModelPolicy)
	logService := services.NewLogService()
	mcpService := services.NewMCPService()
	skillService := services.NewSkillService()
	promptService := services.NewPromptService()
	envCheckService := services.NewEnvCheckService()
	importService := services.NewImportService(providerService, mcpService, geminiService, promptService)
	exportService := services.NewExportService(providerService, geminiService, mcpService, promptService, func() string { return AppVersion })
	deeplinkService := services.NewDeepLinkService(providerService)
	speedTestService := services.NewSpeedTestService()
	connectivityTestService := services.NewConnectivityTestService(providerService, blacklistService, settingsService, defaultModelPolicy)
	healthCheckService := services.NewHealthCheckService(providerService, blacklistService, settingsService, defaultModelPolicy)
	// 初始化健康检查数据库表
	if err := healthCheckService.Start(); err != nil {
		log.Fatalf("初始化健康检查服务失败: %v", err)
	}
	dockService := dock.New()
	versionService := NewVersionService()
	// 把构建期 -ldflags "-X main.UpdatePolicy=..." 注入的策略传给更新服务，
	// 否则该开关只影响 VersionService 的展示，更新流程仍走运行时检测
	updateService := services.NewUpdateService(AppVersion, UpdatePolicy)
	consoleService := services.NewConsoleService()
	customCliService := services.NewCustomCliService(relayConnectAddr, defaultModelPolicy)
	// 网络设置页与 WSL 配置都要按"实际绑定了哪些地址"判断，而不是磁盘上的设置：
	// 监听地址在启动时冻结，改完设置不重启并不会重绑
	networkService := services.NewNetworkService(relayConnectAddr, claudeSettings, codexSettings, geminiService, providerRelay.BoundAddresses)

	// 启动模型元数据后台同步调度（延迟首查 + 每小时检查到期厂商）
	modelSyncService.Start()

	// 启动黑名单自动恢复定时器（每分钟检查一次）
	blacklistStopChan := make(chan struct{})
	services.SafeGo("blacklist-recover-timer", func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// 逐次兜底：单次恢复 panic 只丢这一轮，定时器继续存活
				func() {
					defer services.RecoverAndLog("blacklist-auto-recover")
					if err := blacklistService.AutoRecoverExpired(); err != nil {
						log.Printf("自动恢复黑名单失败: %v", err)
					}
				}()
			case <-blacklistStopChan:
				log.Println("✅ 黑名单定时器已停止")
				return
			}
		}
	})

	// 根据应用设置决定是否启动可用性监控（复用旧的 auto_connectivity_test 字段）。
	// 延迟期间应用可能已经退出，必须能被 OnShutdown 取消，
	// 否则巡检会在 StopBackgroundPolling 之后被重新拉起。
	availabilityStopChan := make(chan struct{})
	services.SafeGo("availability-bootstrap", func() {
		select {
		case <-time.After(3 * time.Second): // 延迟3秒，等待应用初始化
		case <-availabilityStopChan:
			return
		}

		settings, err := appSettings.GetAppSettings()

		// 默认启用自动监控（保持开箱即用）
		autoEnabled := true
		if err != nil {
			log.Printf("读取应用设置失败（使用默认值）: %v", err)
		} else {
			// 读取成功，使用配置值
			autoEnabled = settings.AutoConnectivityTest
		}

		// 应用可能在读配置期间进入关停流程，再次确认后才启动巡检
		select {
		case <-availabilityStopChan:
			return
		default:
		}

		// 旧的 AutoConnectivityTest 字段现在控制可用性监控
		if autoEnabled {
			healthCheckService.SetAutoAvailabilityPolling(true)
			log.Println("✅ 自动可用性监控已启动")
		} else {
			log.Println("ℹ️  自动可用性监控已禁用（可在设置中开启）")
		}
	})

	//fmt.Println(clipboardService)
	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running an macOS.
	appServices := []application.Service{
		application.NewService(appservice),
		application.NewService(providerService),
		application.NewService(settingsService),
		application.NewService(blacklistService),
		application.NewService(claudeSettings),
		application.NewService(codexSettings),
		application.NewService(cliConfigService),
		application.NewService(logService),
		application.NewService(appSettings),
		application.NewService(mcpService),
		application.NewService(skillService),
		application.NewService(promptService),
		application.NewService(envCheckService),
		application.NewService(importService),
		application.NewService(exportService),
		application.NewService(deeplinkService),
		application.NewService(speedTestService),
		application.NewService(connectivityTestService),
		application.NewService(healthCheckService),
		application.NewService(dockService),
		application.NewService(versionService),
		application.NewService(updateService),
		application.NewService(geminiService),
		application.NewService(consoleService),
		application.NewService(customCliService),
		application.NewService(networkService),
		application.NewService(modelSyncService),
		// 前端用 Call.ByName 调 ProviderRelayService.GetAllLastUsedProviders（"最后使用"徽标），
		// 不注册的话该调用永远失败
		application.NewService(providerRelay),
	}
	if suiService != nil {
		appServices = append(appServices, application.NewService(suiService))
	}

	app := application.New(application.Options{
		Name:        "AI Code Studio",
		Description: "Code Switch provider manager",
		Services:    appServices,
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
	})

	// 设置 NotificationService 的 App 引用，用于发送事件到前端
	notificationService.SetApp(app)
	// 窗口 impl 在 app.Run() 内部才创建；启动完成前禁止聚焦类调用
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		appStarted.Store(true)
	})
	// 设置 UpdateService 的 App 引用，用于发送更新事件
	updateService.SetApp(app)
	// 设置 ModelSyncService 的 App 引用，用于广播同步完成事件
	modelSyncService.SetApp(app)

	// 代理放在 App 引用注入之后再启动：早于此的供应商切换事件会因为 app 还是 nil 而丢失。
	// 端口被占用（多开、其它程序占用 18100）时不能直接退出进程——GUI 构建下没有控制台，
	// 用户只会看到"双击没反应"，这里改为弹窗告知并让应用带着降级状态继续启动。
	if relayStartErr := providerRelay.Start(); relayStartErr != nil {
		log.Printf("provider relay start error: %v", relayStartErr)
		listenAddr := providerRelay.Addr()
		// 必须等应用真正跑起来再弹：App.impl 只在 app.Run() 内部赋值，
		// 提前调用 Show() 会在 dispatchOnMainThread 里解引用 nil 直接 panic，
		// 反而比原来的退出更糟。
		app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
			dialog := application.ErrorDialog()
			dialog.SetTitle("代理服务启动失败")
			dialog.SetMessage(fmt.Sprintf(
				"本地代理无法监听 %s：%v\n\n"+
					"常见原因是应用已经在运行，或该端口被其它程序占用。\n"+
					"关闭占用该端口的程序后重启本应用即可恢复转发功能。",
				listenAddr, relayStartErr))
			dialog.Show()
		})
	}

	app.OnShutdown(func() {
		// OnShutdown 由 Wails 经 InvokeSync 派发：内部 panic 会被 handlePanic
		// 吞掉但跳过 wg.Done()，退出流程从此永久挂起（"点退出没反应"）。
		// 必须在这里自兜底，保证 wg.Done 总能执行
		defer services.RecoverAndLog("app-shutdown")
		shuttingDown.Store(true)
		log.Println("🛑 应用正在关闭，停止后台服务...")

		// 1. 停止黑名单定时器，并取消尚在延迟等待的可用性监控启动
		close(blacklistStopChan)
		close(availabilityStopChan)

		// 2. 停止健康检查轮询与连通性测试定时器。
		// 连通性定时器此前被遗漏：它会活过写入队列关闭继续触发 DB 写入
		healthCheckService.StopBackgroundPolling()
		if err := connectivityTestService.Stop(); err != nil {
			log.Printf("connectivity test stop error: %v", err)
		}
		log.Println("✅ 健康检查服务已停止")

		// 2.5 停止模型元数据同步（取消在途请求并等待退出）
		modelSyncService.Stop()
		log.Println("✅ 模型数据同步已停止")

		// 3. 停止代理服务器
		if err := providerRelay.Stop(); err != nil {
			log.Printf("provider relay stop error: %v", err)
		}

		// 3.5 给刚被中断的请求处理协程一点时间把 request_log 塞进写入队列，
		// 否则这些流式请求的 token 与费用会随队列关闭一起丢掉
		time.Sleep(500 * time.Millisecond)

		// 4. 优雅关闭数据库写入队列（10秒超时，双队列架构）
		if err := services.ShutdownGlobalDBQueue(10 * time.Second); err != nil {
			log.Printf("⚠️ 队列关闭超时: %v", err)
		} else {
			// 单次队列统计
			stats1 := services.GetGlobalDBQueueStats()
			log.Printf("✅ 单次队列已关闭，统计：成功=%d 失败=%d 平均延迟=%.2fms",
				stats1.SuccessWrites, stats1.FailedWrites, stats1.AvgLatencyMs)

			// 批量队列统计
			stats2 := services.GetGlobalDBQueueLogsStats()
			log.Printf("✅ 批量队列已关闭，统计：成功=%d 失败=%d 平均延迟=%.2fms（批均分） 批次=%d",
				stats2.SuccessWrites, stats2.FailedWrites, stats2.AvgLatencyMs, stats2.BatchCommits)
		}

		log.Println("✅ 所有后台服务已停止")
	})

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Code Switch R",
		Width:     1400,
		Height:    1040,
		MinWidth:  600,
		MinHeight: 300,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(27, 38, 54),
		URL:              "/",
	})
	var mainWindowCentered bool
	focusMainWindow := func() {
		// Focus() 是 alpha.38 里唯一没有 impl 判空的窗口方法，启动前/退出期
		// 调用必炸；全应用的 Focus 都必须收敛到这个带守卫的入口。
		// recover 兜底覆盖竞态残余窗口
		defer services.RecoverAndLog("focus-main-window")
		if !appStarted.Load() || shuttingDown.Load() {
			return
		}
		if runtime.GOOS == "windows" {
			mainWindow.SetAlwaysOnTop(true)
			mainWindow.Focus()
			services.SafeGo("focus-always-on-top-reset", func() {
				time.Sleep(150 * time.Millisecond)
				if shuttingDown.Load() {
					return
				}
				mainWindow.SetAlwaysOnTop(false)
			})
			return
		}
		mainWindow.Focus()
	}
	showMainWindow := func(withFocus bool) {
		defer services.RecoverAndLog("show-main-window")
		if shuttingDown.Load() {
			return
		}
		if !mainWindowCentered {
			mainWindow.Center()
			mainWindowCentered = true
		}
		if mainWindow.IsMinimised() {
			mainWindow.UnMinimise()
		}
		mainWindow.Show()
		if withFocus {
			focusMainWindow()
		}
		handleDockVisibility(dockService, true)
	}

	showMainWindow(false)
	appservice.showMain = showMainWindow

	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		mainWindow.Hide()
		handleDockVisibility(dockService, false)
		e.Cancel()
	})

	var trayWindow application.Window

	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(event *application.ApplicationEvent) {
		showMainWindow(true)
	})

	app.Event.OnApplicationEvent(events.Mac.ApplicationDidBecomeActive, func(event *application.ApplicationEvent) {
		if trayWindow != nil {
			// Tray exists on macOS; avoid auto-opening the main window on activation.
			return
		}
		if mainWindow.IsVisible() {
			focusMainWindow()
			return
		}
		showMainWindow(true)
	})

	if runtime.GOOS == "darwin" {
		trayWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:            "Code Switch Tray",
			Name:             "tray",
			Width:            trayWindowWidth,
			Height:           trayWindowMinHeight,
			MinWidth:         trayWindowWidth,
			MaxWidth:         trayWindowWidth,
			MinHeight:        trayWindowMinHeight,
			MaxHeight:        trayWindowMaxHeight,
			AlwaysOnTop:      true,
			DisableResize:    true,
			Frameless:        true,
			Hidden:           true,
			BackgroundType:   application.BackgroundTypeTransparent,
			BackgroundColour: application.NewRGBA(0, 0, 0, 0),
			Mac: application.MacWindow{
				Backdrop:      application.MacBackdropTransparent,
				TitleBar:      application.MacTitleBarHidden,
				DisableShadow: true,
				WindowLevel:   application.MacWindowLevelPopUpMenu,
			},
			URL: "/#/tray",
		})
		trayWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
			trayWindow.Hide()
			e.Cancel()
		})
		appservice.TrayWindow = trayWindow
	}

	systray := app.SystemTray.New()
	// systray.SetLabel("AI Code Studio")
	systray.SetTooltip("AI Code Studio")
	if lightIcon := loadTrayIcon("assets/icon.png"); len(lightIcon) > 0 {
		systray.SetIcon(lightIcon)
	}
	if darkIcon := loadTrayIcon("assets/icon-dark.png"); len(darkIcon) > 0 {
		systray.SetDarkModeIcon(darkIcon)
	}

	if runtime.GOOS == "darwin" && trayWindow != nil {
		// 托盘弹窗开关：关闭时点击托盘图标直接打开主窗口。
		// systray 只在启动期绑定弹窗（AttachWindow 无法运行时解绑），
		// 因此关→开需要重启应用才生效；开→关由前端 Tray 页即时兜底。
		enableTrayPopup := true
		if settings, err := appSettings.GetAppSettings(); err == nil {
			enableTrayPopup = settings.EnableTrayPopup
		}

		trayMenu := application.NewMenu()
		trayMenu.Add("显示主窗口").OnClick(func(ctx *application.Context) {
			showMainWindow(true)
		})
		trayMenu.Add("退出").OnClick(func(ctx *application.Context) {
			app.Quit()
		})
		systray.SetMenu(trayMenu)

		if enableTrayPopup {
			systray.AttachWindow(trayWindow).WindowOffset(8)
		} else {
			systray.OnClick(func() {
				defer services.RecoverAndLog("tray-click")
				showMainWindow(true)
			})
		}

		systray.OnRightClick(func() {
			defer services.RecoverAndLog("tray-right-click")
			systray.OpenMenu()
		})
	} else {
		refreshTrayMenu := func() {
			used, total := getTrayUsage(logService, appSettings)
			trayMenu := buildUsageTrayMenu(used, total, func() {
				showMainWindow(true)
			}, func() {
				app.Quit()
			})
			systray.SetMenu(trayMenu)
		}
		refreshTrayMenu()
		// 托盘回调运行在 Win32 消息循环线程上且 Wails 对左键路径没有兜底
		// （右键/菜单路径有 handlePanic，左键 clickHandler 是裸调用），
		// 任何 panic 都会直接杀进程，必须自兜底
		systray.OnRightClick(func() {
			defer services.RecoverAndLog("tray-right-click")
			refreshTrayMenu()
			systray.OpenMenu()
		})
		systray.OnClick(func() {
			defer services.RecoverAndLog("tray-click")
			if shuttingDown.Load() {
				return
			}
			if !mainWindow.IsVisible() {
				showMainWindow(true)
				return
			}
			if !mainWindow.IsFocused() {
				focusMainWindow()
			}
		})
	}

	appservice.setApp(app)

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}

func loadTrayIcon(path string) []byte {
	data, err := trayIcons.ReadFile(path)
	if err != nil {
		log.Printf("failed to load tray icon %s: %v", path, err)
		return nil
	}
	return data
}

func handleDockVisibility(service *dock.DockService, show bool) {
	if runtime.GOOS != "darwin" || service == nil {
		return
	}
	if show {
		service.ShowAppIcon()
	} else {
		service.HideAppIcon()
	}
}

const (
	trayWindowWidth      = 360
	trayWindowMinHeight  = 120
	trayWindowMaxHeight  = 420
	trayProgressBarWidth = 28
)

// getTrayUsage 汇总原生托盘菜单要显示的今日用量与预算。
// 预算按平台分别配置（budget_total 对 claude、budget_total_codex 对 codex，
// 前端托盘同样按平台取用），所以成本也必须按平台取，
// 不能拿"全平台总成本"去除以"claude 单平台预算"。
// 只统计配置了预算的平台；一个都没配时退回全平台成本、预算显示为未设置。
func getTrayUsage(logService *services.LogService, appSettings *services.AppSettingsService) (float64, float64) {
	if logService == nil {
		return 0, 0
	}

	platformCost := func(platform string) float64 {
		stats, err := logService.StatsSince(platform)
		if err != nil {
			return 0
		}
		return stats.CostTotal
	}

	if appSettings == nil {
		return 0, 0
	}
	settings, err := appSettings.GetAppSettings()
	if err != nil {
		return 0, 0
	}

	budgeted := []struct {
		platform   string
		total      float64
		adjustment float64
	}{
		{"claude", settings.BudgetTotal, settings.BudgetUsedAdjustment},
		{"codex", settings.BudgetTotalCodex, settings.BudgetUsedAdjustmentCodex},
	}

	used := 0.0
	total := 0.0
	matched := false
	for _, b := range budgeted {
		if b.total <= 0 {
			continue
		}
		matched = true
		total += b.total
		used += platformCost(b.platform) + b.adjustment
	}

	if !matched {
		// 未设置任何预算：只展示今日总成本，进度显示为"未设置"
		used = platformCost("")
	}

	if used < 0 {
		used = 0
	}
	if total < 0 {
		total = 0
	}
	return used, total
}

func buildUsageTrayMenu(used float64, total float64, onShow func(), onQuit func()) *application.Menu {
	menu := application.NewMenu()
	menu.Add(trayUsageLabel(used, total)).SetEnabled(false)
	menu.Add(trayProgressLabel(used, total)).SetEnabled(false)
	menu.AddSeparator()
	menu.Add("显示主窗口").OnClick(func(ctx *application.Context) {
		onShow()
	})
	menu.Add("退出").OnClick(func(ctx *application.Context) {
		onQuit()
	})
	return menu
}

func trayUsageLabel(used float64, total float64) string {
	usedLabel := formatCurrency(used)
	if total <= 0 {
		return fmt.Sprintf("今日已用 %s / 未设置", usedLabel)
	}
	return fmt.Sprintf("今日已用 %s / %s", usedLabel, formatCurrency(total))
}

func trayProgressLabel(used float64, total float64) string {
	bar := strings.Repeat("-", trayProgressBarWidth)
	if total <= 0 {
		return fmt.Sprintf("进度 [%s] --%%", bar)
	}
	ratio := used / total
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(math.Round(ratio * float64(trayProgressBarWidth)))
	if filled < 0 {
		filled = 0
	}
	if filled > trayProgressBarWidth {
		filled = trayProgressBarWidth
	}
	bar = strings.Repeat("#", filled) + strings.Repeat("-", trayProgressBarWidth-filled)
	percent := int(math.Round(ratio * 100))
	return fmt.Sprintf("进度 [%s] %d%%", bar, percent)
}

func formatCurrency(value float64) string {
	return fmt.Sprintf("$%.2f", value)
}
