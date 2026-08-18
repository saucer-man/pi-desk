package main

import (
	"embed"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"pi-desk/internal/appservice"
	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/repository"
	"pi-desk/internal/sessionindex"
	"pi-desk/internal/workspace"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/windows/icon.ico
var trayIcon []byte

func centeredWindowState(width, height int, screens []*application.Screen) domain.WindowState {
	var target *application.Screen
	for _, screen := range screens {
		if screen != nil && screen.IsPrimary && screen.WorkArea.Width > 0 && screen.WorkArea.Height > 0 {
			target = screen
			break
		}
	}
	if target == nil {
		for _, screen := range screens {
			if screen != nil && screen.WorkArea.Width > 0 && screen.WorkArea.Height > 0 {
				target = screen
				break
			}
		}
	}
	if target == nil {
		return domain.WindowState{}
	}
	workArea := target.WorkArea
	width = min(width, workArea.Width)
	height = min(height, workArea.Height)
	return domain.WindowState{
		X: workArea.X + (workArea.Width-width)/2, Y: workArea.Y + (workArea.Height-height)/2,
		Width: width, Height: height, Valid: true,
	}
}

const (
	defaultWindowWidth  = 1440
	defaultWindowHeight = 900
	minimumWindowWidth  = 980
	minimumWindowHeight = 680
)

func constrainWindowState(state domain.WindowState, screens []*application.Screen) domain.WindowState {
	if !state.Valid || state.Width <= 0 || state.Height <= 0 {
		return state
	}

	var target *application.Screen
	var bestOverlap int64
	for _, screen := range screens {
		if screen == nil || screen.WorkArea.Width <= 0 || screen.WorkArea.Height <= 0 {
			continue
		}
		overlap := rectOverlapArea(state, screen.WorkArea)
		if overlap > bestOverlap {
			bestOverlap = overlap
			target = screen
		}
	}

	detached := target == nil
	if detached {
		for _, screen := range screens {
			if screen != nil && screen.IsPrimary && screen.WorkArea.Width > 0 && screen.WorkArea.Height > 0 {
				target = screen
				break
			}
		}
	}
	if target == nil {
		for _, screen := range screens {
			if screen != nil && screen.WorkArea.Width > 0 && screen.WorkArea.Height > 0 {
				target = screen
				break
			}
		}
	}
	if target == nil {
		return state
	}

	workArea := target.WorkArea
	state.Width = min(max(state.Width, minimumWindowWidth), workArea.Width)
	state.Height = min(max(state.Height, minimumWindowHeight), workArea.Height)
	if detached {
		state.X = workArea.X + (workArea.Width-state.Width)/2
		state.Y = workArea.Y + (workArea.Height-state.Height)/2
		return state
	}
	state.X = max(workArea.X, min(state.X, workArea.X+workArea.Width-state.Width))
	state.Y = max(workArea.Y, min(state.Y, workArea.Y+workArea.Height-state.Height))
	return state
}

// shouldMaximiseFirstLaunch avoids opening an almost full normal window on a
// compact logical work area, which is common on high-DPI Windows displays.
func shouldMaximiseFirstLaunch(state domain.WindowState, screens []*application.Screen) bool {
	if !state.Valid || state.Width <= 0 || state.Height <= 0 {
		return false
	}
	for _, screen := range screens {
		if screen == nil || !screen.IsPrimary || screen.WorkArea.Width <= 0 || screen.WorkArea.Height <= 0 {
			continue
		}
		return state.Width*100 >= screen.WorkArea.Width*92 || state.Height*100 >= screen.WorkArea.Height*92
	}
	return false
}

func rectOverlapArea(state domain.WindowState, area application.Rect) int64 {
	left := max(int64(state.X), int64(area.X))
	top := max(int64(state.Y), int64(area.Y))
	right := min(int64(state.X)+int64(state.Width), int64(area.X)+int64(area.Width))
	bottom := min(int64(state.Y)+int64(state.Height), int64(area.Y)+int64(area.Height))
	if right <= left || bottom <= top {
		return 0
	}
	return (right - left) * (bottom - top)
}

func main() {
	notificationService := notifications.New()
	locator := piruntime.NewLocator()
	statePath, err := workspace.DefaultStatePath()
	if err != nil {
		log.Fatal(err)
	}
	sessionsPath, err := sessionindex.DefaultRoot()
	if err != nil {
		log.Fatal(err)
	}
	catalog := workspace.NewCatalog(statePath)
	desktopService := appservice.NewDesktopService(locator, catalog)
	sessionIndex := sessionindex.New(sessionsPath)
	agentService := appservice.NewAgentService(locator, sessionIndex)
	piMaintenanceService := appservice.NewPiMaintenanceService(locator, agentService)
	modelConfigService := appservice.NewModelConfigService()
	promptTemplateService := appservice.NewPromptTemplateService(catalog)
	managedSkillService := appservice.NewManagedSkillService(catalog)
	piExtensionService := appservice.NewPiExtensionService()
	mcpConfigService := appservice.NewMcpConfigService(catalog)
	catalogService := appservice.NewCatalogService(catalog, sessionIndex)
	repositoryService := appservice.NewRepositoryService(catalog, repository.New())
	terminalService := appservice.NewTerminalService(catalog)

	app := application.New(application.Options{
		Name:        "Pi Desk",
		Description: "A desktop interface for the Pi coding agent",
		Services: []application.Service{
			application.NewService(notificationService),
			application.NewService(desktopService),
			application.NewService(agentService),
			application.NewService(piMaintenanceService),
			application.NewService(modelConfigService),
			application.NewService(promptTemplateService),
			application.NewService(managedSkillService),
			application.NewService(piExtensionService),
			application.NewService(mcpConfigService),
			application.NewService(catalogService),
			application.NewService(repositoryService),
			application.NewService(terminalService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	screens := app.Screen.GetAll()
	windowState := centeredWindowState(defaultWindowWidth, defaultWindowHeight, screens)
	restoreMaximized := false
	firstLaunchMaximized := false
	if saved, loadErr := catalog.Window(); loadErr == nil && saved.Valid {
		windowState = domain.WindowState{X: saved.X, Y: saved.Y, Width: saved.Width, Height: saved.Height, Maximized: saved.Maximized, Valid: true}
		restoreMaximized = saved.Maximized
	} else if shouldMaximiseFirstLaunch(windowState, screens) {
		windowState.Maximized = true
		firstLaunchMaximized = true
	}
	windowState = constrainWindowState(windowState, screens)
	windowOptions := application.WebviewWindowOptions{
		Name:            "main",
		Title:           "Pi Desk",
		Width:           defaultWindowWidth,
		Height:          defaultWindowHeight,
		MinWidth:        minimumWindowWidth,
		MinHeight:       minimumWindowHeight,
		Frameless:       runtime.GOOS == "windows",
		DevToolsEnabled: true,
		Hidden:          restoreMaximized && runtime.GOOS == "windows",
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 46,
			Backdrop:                application.MacBackdropNormal,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(24, 24, 24),
		URL:              "/",
	}
	if windowState.Valid {
		windowOptions.Width, windowOptions.Height = windowState.Width, windowState.Height
		windowOptions.InitialPosition = application.WindowXY
		windowOptions.X, windowOptions.Y = windowState.X, windowState.Y
	}
	// Wails v3 beta currently processes StartState before restored X/Y on
	// Windows. A first launch has no prior normal bounds, so it can start
	// maximised directly. Restored maximised windows still use the runtime-ready
	// path below after their saved normal bounds have been applied.
	if firstLaunchMaximized || (restoreMaximized && runtime.GOOS != "windows") {
		windowOptions.StartState = application.WindowStateMaximised
	}
	window := app.Window.NewWithOptions(windowOptions)
	if restoreMaximized && runtime.GOOS == "windows" {
		window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
			window.SetBounds(application.Rect{X: windowState.X, Y: windowState.Y, Width: windowState.Width, Height: windowState.Height})
			time.AfterFunc(100*time.Millisecond, func() {
				window.Maximise()
				window.Show()
				window.Focus()
			})
		})
	}

	var windowSaveMu sync.Mutex
	var windowSaveTimer *time.Timer
	lastNormal := windowState
	if !lastNormal.Valid {
		lastNormal = domain.WindowState{Width: windowOptions.Width, Height: windowOptions.Height, Valid: true}
	}
	saveWindowState := func(immediate bool) {
		windowSaveMu.Lock()
		if windowSaveTimer != nil {
			windowSaveTimer.Stop()
		}
		persist := func() {
			windowSaveMu.Lock()
			defer windowSaveMu.Unlock()
			maximized := window.IsMaximised()
			if !maximized {
				bounds := window.Bounds()
				if bounds.Width >= minimumWindowWidth && bounds.Height >= minimumWindowHeight {
					lastNormal = constrainWindowState(domain.WindowState{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height, Valid: true}, app.Screen.GetAll())
				}
			}
			state := lastNormal
			state.Maximized = maximized
			if saveErr := desktopService.SaveWindowState(state); saveErr != nil {
				log.Printf("save window state: %v", saveErr)
			}
		}
		if immediate {
			windowSaveMu.Unlock()
			persist()
			return
		}
		windowSaveTimer = time.AfterFunc(300*time.Millisecond, persist)
		windowSaveMu.Unlock()
	}
	for _, eventType := range []events.WindowEventType{
		events.Common.WindowDidMove,
		events.Common.WindowDidResize,
		events.Common.WindowMaximise,
		events.Common.WindowUnMaximise,
	} {
		window.OnWindowEvent(eventType, func(*application.WindowEvent) { saveWindowState(false) })
	}
	var explicitQuit atomic.Bool
	showWindow := func() {
		if !window.IsMaximised() {
			bounds := window.Bounds()
			visible := constrainWindowState(domain.WindowState{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: bounds.Height, Valid: true}, app.Screen.GetAll())
			window.SetBounds(application.Rect{X: visible.X, Y: visible.Y, Width: visible.Width, Height: visible.Height})
		}
		window.Show()
		if window.IsMinimised() {
			window.UnMinimise()
		}
		window.Focus()
	}
	notificationService.OnNotificationResponse(func(result notifications.NotificationResult) {
		if result.Error != nil {
			log.Printf("notification response: %v", result.Error)
			return
		}
		showWindow()
	})
	systemTray := app.SystemTray.New()
	systemTray.SetIcon(trayIcon)
	systemTray.SetTooltip("Pi Desk")
	trayMenu := app.NewMenu()
	trayMenu.Add("显示 Pi Desk").OnClick(func(*application.Context) { showWindow() })
	trayMenu.Add("退出 Pi Desk").OnClick(func(*application.Context) {
		explicitQuit.Store(true)
		app.Quit()
	})
	systemTray.SetMenu(trayMenu)
	systemTray.OnClick(showWindow)
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		saveWindowState(true)
		if explicitQuit.Load() {
			return
		}
		window.Hide()
		event.Cancel()
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
