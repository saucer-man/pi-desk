//go:build windows

package workspaceapp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func discoverApplications() []candidate {
	localAppData := os.Getenv("LOCALAPPDATA")
	programFiles := os.Getenv("ProgramFiles")
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	systemRoot := os.Getenv("SystemRoot")

	vscode := firstExecutable(
		[]string{"Code.exe"},
		absolutePaths(
			filepath.Join(localAppData, "Programs", "Microsoft VS Code", "Code.exe"),
			filepath.Join(programFiles, "Microsoft VS Code", "Code.exe"),
			filepath.Join(programFilesX86, "Microsoft VS Code", "Code.exe"),
		),
	)
	vscodeInsiders := firstExecutable(
		[]string{"Code - Insiders.exe"},
		absolutePaths(
			filepath.Join(localAppData, "Programs", "Microsoft VS Code Insiders", "Code - Insiders.exe"),
			filepath.Join(programFiles, "Microsoft VS Code Insiders", "Code - Insiders.exe"),
		),
	)
	pyCharm := firstExecutable(nil, pyCharmWindowsPaths(false, localAppData, programFiles, programFilesX86))
	pyCharmCommunity := firstExecutable([]string{"pycharm-community.exe"}, pyCharmWindowsPaths(true, localAppData, programFiles, programFilesX86))
	if pathPyCharm := firstExecutable([]string{"pycharm64.exe", "pycharm.exe"}, nil); pathPyCharm != "" {
		if isPyCharmCommunityPath(pathPyCharm) {
			pyCharmCommunity = pathPyCharm
		} else {
			pyCharm = pathPyCharm
		}
	}
	cursor := firstExecutable(
		[]string{"Cursor.exe", "cursor.exe"},
		absolutePaths(
			filepath.Join(localAppData, "Programs", "cursor", "Cursor.exe"),
			filepath.Join(localAppData, "Programs", "Cursor", "Cursor.exe"),
			filepath.Join(programFiles, "Cursor", "Cursor.exe"),
		),
	)
	explorer := firstExecutable(
		[]string{"explorer.exe"},
		absolutePaths(filepath.Join(systemRoot, "explorer.exe")),
	)

	var result []candidate
	result = appendCandidateWithIcons(result, VSCodeID, "Visual Studio Code", vscode, electronWindowsIconPaths(vscode, "code.ico"))
	result = appendCandidateWithIcons(result, VSCodeInsidersID, "Visual Studio Code Insiders", vscodeInsiders, electronWindowsIconPaths(vscodeInsiders, "code.ico"))
	result = appendCandidateWithIcons(result, PyCharmID, "PyCharm Professional", pyCharm, adjacentIconPaths(pyCharm, "pycharm.ico", "pycharm.png"))
	result = appendCandidateWithIcons(result, PyCharmCommunityID, "PyCharm Community", pyCharmCommunity, adjacentIconPaths(pyCharmCommunity, "pycharm.ico", "pycharm.png"))
	result = appendCandidateWithIcons(result, CursorID, "Cursor", cursor, electronWindowsIconPaths(cursor, "code.ico"))
	result = appendCandidate(result, FileManagerID, "File Explorer", explorer)
	return result
}

func absolutePaths(paths ...string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if filepath.IsAbs(path) {
			result = append(result, path)
		}
	}
	return result
}

func electronWindowsIconPaths(executable, iconName string) []string {
	if executable == "" {
		return nil
	}
	root := filepath.Dir(executable)
	patterns := []string{
		filepath.Join(root, "resources", "app", "resources", "win32", iconName),
		filepath.Join(root, "*", "resources", "app", "resources", "win32", iconName),
	}
	return newestGlobMatches(patterns...)
}

func adjacentIconPaths(executable string, names ...string) []string {
	if executable == "" {
		return nil
	}
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, filepath.Join(filepath.Dir(executable), name))
	}
	return result
}

func pyCharmWindowsPaths(community bool, roots ...string) []string {
	var matches []string
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			continue
		}
		patterns := []string{
			filepath.Join(root, "JetBrains", "PyCharm*", "bin", "pycharm64.exe"),
			filepath.Join(root, "Programs", "PyCharm*", "bin", "pycharm64.exe"),
			filepath.Join(root, "JetBrains", "Toolbox", "apps", "*", "bin", "pycharm64.exe"),
			filepath.Join(root, "JetBrains", "Toolbox", "apps", "*", "*", "*", "bin", "pycharm64.exe"),
		}
		matches = append(matches, newestGlobMatches(patterns...)...)
	}
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		if isPyCharmCommunityPath(match) == community {
			result = append(result, match)
		}
	}
	return result
}

func isPyCharmCommunityPath(path string) bool {
	path = strings.ToLower(path)
	return strings.Contains(path, "community") || strings.Contains(path, "pycharm-c")
}

func newestGlobMatches(patterns ...string) []string {
	var matches []string
	for _, pattern := range patterns {
		found, _ := filepath.Glob(pattern)
		matches = append(matches, found...)
	}
	slices.Sort(matches)
	slices.Reverse(matches)
	return slices.Compact(matches)
}
