//go:build linux

package workspaceapp

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type desktopEntry struct {
	path string
	name string
	icon string
}

func discoverApplications() []candidate {
	home, _ := os.UserHomeDir()
	desktopRoots := []string{
		filepath.Join(home, ".local", "share", "applications"),
		"/usr/local/share/applications",
		"/usr/share/applications",
		"/var/lib/snapd/desktop/applications",
	}

	vscodeEntry := firstDesktopEntry(desktopRoots, "code.desktop", "visual-studio-code.desktop")
	vscodeInsidersEntry := firstDesktopEntry(desktopRoots, "code-insiders.desktop", "visual-studio-code-insiders.desktop")
	cursorEntry := firstDesktopEntry(desktopRoots, "cursor.desktop", "cursor-cursor.desktop")
	pyCharmEntry := firstPyCharmDesktopEntry(desktopRoots, false)
	pyCharmCommunityEntry := firstPyCharmDesktopEntry(desktopRoots, true)
	fileManagerEntry := defaultFileManagerDesktopEntry(desktopRoots)

	vscode := firstExecutable([]string{"code"}, nil)
	vscodeInsiders := firstExecutable([]string{"code-insiders"}, nil)
	pyCharm := firstExecutable([]string{"pycharm", "pycharm-professional"}, []string{
		filepath.Join(home, ".local", "share", "JetBrains", "Toolbox", "scripts", "pycharm"),
		"/snap/bin/pycharm-professional",
	})
	pyCharmCommunity := firstExecutable([]string{"pycharm-community"}, []string{"/snap/bin/pycharm-community"})
	cursor := firstExecutable([]string{"cursor"}, []string{"/opt/Cursor/cursor", "/usr/bin/cursor"})
	fileManager := firstExecutable([]string{"xdg-open"}, []string{"/usr/bin/xdg-open"})

	var result []candidate
	result = appendCandidateWithIcons(result, VSCodeID, "Visual Studio Code", vscode, desktopEntryIconPaths(vscodeEntry, home))
	result = appendCandidateWithIcons(result, VSCodeInsidersID, "Visual Studio Code Insiders", vscodeInsiders, desktopEntryIconPaths(vscodeInsidersEntry, home))
	result = appendCandidateWithIcons(result, PyCharmID, "PyCharm Professional", pyCharm, desktopEntryIconPaths(pyCharmEntry, home))
	result = appendCandidateWithIcons(result, PyCharmCommunityID, "PyCharm Community", pyCharmCommunity, desktopEntryIconPaths(pyCharmCommunityEntry, home))
	result = appendCandidateWithIcons(result, CursorID, "Cursor", cursor, desktopEntryIconPaths(cursorEntry, home))
	result = appendCandidateWithIcons(result, FileManagerID, desktopEntryName(fileManagerEntry, "Files"), fileManager, desktopEntryIconPaths(fileManagerEntry, home))
	return result
}

func desktopEntryName(entry desktopEntry, fallback string) string {
	if strings.TrimSpace(entry.name) != "" {
		return entry.name
	}
	return fallback
}

func firstDesktopEntry(roots []string, names ...string) desktopEntry {
	for _, root := range roots {
		for _, name := range names {
			if entry, ok := readDesktopEntry(filepath.Join(root, name)); ok {
				return entry
			}
		}
	}
	return desktopEntry{}
}

func firstPyCharmDesktopEntry(roots []string, community bool) desktopEntry {
	var paths []string
	for _, root := range roots {
		for _, pattern := range []string{"*pycharm*.desktop", "jetbrains-pycharm*.desktop"} {
			matches, _ := filepath.Glob(filepath.Join(root, pattern))
			paths = append(paths, matches...)
		}
	}
	slices.Sort(paths)
	slices.Reverse(paths)
	for _, path := range slices.Compact(paths) {
		entry, ok := readDesktopEntry(path)
		if !ok {
			continue
		}
		value := strings.ToLower(path + " " + entry.name)
		isCommunity := strings.Contains(value, "community") || strings.Contains(value, "pycharm-c")
		if isCommunity == community {
			return entry
		}
	}
	return desktopEntry{}
}

func defaultFileManagerDesktopEntry(roots []string) desktopEntry {
	executable, err := exec.LookPath("xdg-mime")
	if err != nil || !filepath.IsAbs(executable) {
		return desktopEntry{}
	}
	output, err := exec.Command(executable, "query", "default", "inode/directory").Output()
	if err != nil {
		return desktopEntry{}
	}
	name := strings.TrimSpace(string(output))
	if name == "" || filepath.Base(name) != name {
		return desktopEntry{}
	}
	return firstDesktopEntry(roots, name)
}

func readDesktopEntry(path string) (desktopEntry, bool) {
	file, err := os.Open(path)
	if err != nil {
		return desktopEntry{}, false
	}
	defer file.Close()
	entry := desktopEntry{path: path}
	inDesktopGroup := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 128<<10)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inDesktopGroup = line == "[Desktop Entry]"
			continue
		}
		if !inDesktopGroup || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "Name":
			if entry.name == "" {
				entry.name = strings.TrimSpace(value)
			}
		case "Icon":
			entry.icon = strings.TrimSpace(value)
		}
	}
	return entry, scanner.Err() == nil && entry.icon != ""
}

func desktopEntryIconPaths(entry desktopEntry, home string) []string {
	icon := strings.TrimSpace(entry.icon)
	if icon == "" {
		return nil
	}
	if filepath.IsAbs(icon) {
		return []string{icon}
	}
	name := strings.TrimSuffix(icon, filepath.Ext(icon))
	var result []string
	for _, root := range []string{
		filepath.Join(home, ".local", "share", "icons"),
		filepath.Join(home, ".icons"),
		"/usr/local/share/icons",
		"/usr/share/icons",
		"/var/lib/snapd/desktop/icons",
	} {
		for _, size := range []string{"512x512", "256x256", "128x128", "64x64", "48x48"} {
			matches, _ := filepath.Glob(filepath.Join(root, "*", size, "apps", name+".png"))
			result = append(result, matches...)
		}
		matches, _ := filepath.Glob(filepath.Join(root, "*", "scalable", "apps", name+".svg"))
		result = append(result, matches...)
	}
	for _, root := range []string{"/usr/local/share/pixmaps", "/usr/share/pixmaps"} {
		result = append(result, filepath.Join(root, name+".png"), filepath.Join(root, name+".svg"))
	}
	return result
}
