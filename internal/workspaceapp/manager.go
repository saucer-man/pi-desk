package workspaceapp

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
)

const (
	VSCodeID             = "vscode"
	VSCodeInsidersID     = "vscode-insiders"
	PyCharmID            = "pycharm"
	PyCharmCommunityID   = "pycharm-community"
	CursorID             = "cursor"
	FileManagerID        = "file-manager"
	workspaceIconSize    = 64
	workspaceIconPadding = 4
	maxIconDataURLBytes  = 256 << 10
)

// Application is a host-discovered, fixed workspace opener. Executable paths
// are intentionally kept out of the frontend contract.
type Application struct {
	ID          string
	Name        string
	IconDataURL string
}

type candidate struct {
	Application
	executable string
	prefixArgs []string
	iconPaths  []string
}

type iconLoader func(candidate) (image.Image, error)

type Manager struct {
	discover func() []candidate
	start    func(string, ...string) error
	loadIcon iconLoader

	iconMu    sync.Mutex
	iconCache map[string]string
}

func NewManager() *Manager {
	return &Manager{
		discover:  discoverApplications,
		start:     startCommand,
		loadIcon:  loadNativeIcon,
		iconCache: make(map[string]string),
	}
}

func (manager *Manager) List() []Application {
	if manager == nil || manager.discover == nil {
		return nil
	}
	candidates := manager.discover()
	result := make([]Application, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, item := range candidates {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.executable) == "" {
			continue
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		iconDataURL := manager.applicationIcon(item)
		if iconDataURL == "" && item.ID != FileManagerID {
			// Branded applications without a real or reviewed fallback icon do
			// not enter the menu. A generic glyph would misrepresent the app.
			continue
		}
		seen[item.ID] = struct{}{}
		result = append(result, Application{ID: item.ID, Name: item.Name, IconDataURL: iconDataURL})
	}
	return result
}

func (manager *Manager) Open(applicationID, workspacePath string) error {
	if manager == nil || manager.discover == nil || manager.start == nil {
		return errors.New("workspace application manager is unavailable")
	}
	applicationID = strings.TrimSpace(applicationID)
	if applicationID == "" {
		return errors.New("workspace application id is required")
	}
	workspacePath = strings.TrimSpace(workspacePath)
	info, err := os.Stat(workspacePath)
	if err != nil {
		return fmt.Errorf("inspect workspace before opening: %w", err)
	}
	if !info.IsDir() {
		return errors.New("workspace path is not a directory")
	}
	for _, item := range manager.discover() {
		if item.ID != applicationID || strings.TrimSpace(item.executable) == "" {
			continue
		}
		args := append([]string{}, item.prefixArgs...)
		args = append(args, filepath.Clean(workspacePath))
		if err := manager.start(item.executable, args...); err != nil {
			return fmt.Errorf("open workspace with %s: %w", item.Name, err)
		}
		return nil
	}
	return errors.New("workspace application is not installed")
}

func (manager *Manager) applicationIcon(item candidate) string {
	cacheKey := item.ID + "\x00" + item.executable + "\x00" + strings.Join(item.iconPaths, "\x00")
	manager.iconMu.Lock()
	if manager.iconCache == nil {
		manager.iconCache = make(map[string]string)
	}
	if cached, exists := manager.iconCache[cacheKey]; exists {
		manager.iconMu.Unlock()
		return cached
	}
	manager.iconMu.Unlock()

	var icon image.Image
	if manager.loadIcon != nil {
		icon, _ = manager.loadIcon(item)
	}
	if icon == nil {
		icon, _ = fallbackIcon(item.ID)
	}
	dataURL := iconDataURL(icon)

	manager.iconMu.Lock()
	manager.iconCache[cacheKey] = dataURL
	manager.iconMu.Unlock()
	return dataURL
}

func iconDataURL(source image.Image) string {
	if source == nil {
		return ""
	}
	normalized := normalizeIcon(source)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, normalized); err != nil || encoded.Len() == 0 {
		return ""
	}
	value := "data:image/png;base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
	if len(value) > maxIconDataURLBytes {
		return ""
	}
	return value
}

func normalizeIcon(source image.Image) *image.NRGBA {
	bounds := visibleBounds(source)
	canvas := image.NewNRGBA(image.Rect(0, 0, workspaceIconSize, workspaceIconSize))
	if bounds.Empty() {
		return canvas
	}
	available := workspaceIconSize - 2*workspaceIconPadding
	scale := min(float64(available)/float64(bounds.Dx()), float64(available)/float64(bounds.Dy()))
	width := max(1, int(float64(bounds.Dx())*scale+0.5))
	height := max(1, int(float64(bounds.Dy())*scale+0.5))
	destination := image.Rect((workspaceIconSize-width)/2, (workspaceIconSize-height)/2, (workspaceIconSize+width)/2, (workspaceIconSize+height)/2)
	xdraw.CatmullRom.Scale(canvas, destination, source, bounds, xdraw.Over, nil)
	return canvas
}

func visibleBounds(source image.Image) image.Rectangle {
	bounds := source.Bounds()
	left, top, right, bottom := bounds.Max.X, bounds.Max.Y, bounds.Min.X, bounds.Min.Y
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			_, _, _, alpha := source.At(x, y).RGBA()
			if alpha <= 0x0800 {
				continue
			}
			left = min(left, x)
			top = min(top, y)
			right = max(right, x+1)
			bottom = max(bottom, y+1)
		}
	}
	if right <= left || bottom <= top {
		return image.Rectangle{}
	}
	return image.Rect(left, top, right, bottom)
}

func startCommand(executable string, args ...string) error {
	command := exec.Command(executable, args...)
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

func firstExecutable(names []string, paths []string) string {
	for _, name := range names {
		if executable, err := exec.LookPath(name); err == nil && filepath.IsAbs(executable) {
			return executable
		}
	}
	for _, path := range paths {
		if filepath.IsAbs(path) && isExecutableFile(path) {
			return filepath.Clean(path)
		}
	}
	return ""
}

func isExecutableFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

func appendCandidate(result []candidate, id, name, executable string, prefixArgs ...string) []candidate {
	return appendCandidateWithIcons(result, id, name, executable, nil, prefixArgs...)
}

func appendCandidateWithIcons(result []candidate, id, name, executable string, iconPaths []string, prefixArgs ...string) []candidate {
	if executable == "" {
		return result
	}
	return append(result, candidate{
		Application: Application{ID: id, Name: name},
		executable:  executable,
		prefixArgs:  prefixArgs,
		iconPaths:   iconPaths,
	})
}
