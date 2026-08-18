//go:build darwin

package workspaceapp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func discoverApplications() []candidate {
	home, _ := os.UserHomeDir()
	applicationRoots := []string{"/Applications", filepath.Join(home, "Applications")}

	vscodeApps := applicationPaths(applicationRoots, []string{"Visual Studio Code.app"})
	vscodeInsidersApps := applicationPaths(applicationRoots, []string{"Visual Studio Code - Insiders.app"})
	cursorApps := applicationPaths(applicationRoots, []string{"Cursor.app"})
	pyCharmApps := pyCharmDarwinApplications(applicationRoots, false)
	pyCharmCommunityApps := pyCharmDarwinApplications(applicationRoots, true)

	vscode := firstExecutable([]string{"code"}, bundleExecutables(vscodeApps, "Contents/Resources/app/bin/code", "Contents/MacOS/Electron"))
	vscodeInsiders := firstExecutable([]string{"code-insiders"}, bundleExecutables(vscodeInsidersApps, "Contents/Resources/app/bin/code", "Contents/MacOS/Electron"))
	pyCharm := firstExecutable([]string{"pycharm"}, bundleExecutables(pyCharmApps, "Contents/MacOS/pycharm"))
	pyCharmCommunity := firstExecutable([]string{"pycharm-community"}, bundleExecutables(pyCharmCommunityApps, "Contents/MacOS/pycharm"))
	cursor := firstExecutable([]string{"cursor"}, bundleExecutables(cursorApps, "Contents/Resources/app/bin/cursor", "Contents/MacOS/Cursor"))
	open := firstExecutable([]string{"open"}, []string{"/usr/bin/open"})

	var result []candidate
	result = appendCandidateWithIcons(result, VSCodeID, "Visual Studio Code", vscode, bundleIcons(vscodeApps, "Code.icns"))
	result = appendCandidateWithIcons(result, VSCodeInsidersID, "Visual Studio Code Insiders", vscodeInsiders, bundleIcons(vscodeInsidersApps, "Code.icns"))
	result = appendCandidateWithIcons(result, PyCharmID, "PyCharm Professional", pyCharm, bundleIcons(pyCharmApps, "pycharm.icns"))
	result = appendCandidateWithIcons(result, PyCharmCommunityID, "PyCharm Community", pyCharmCommunity, bundleIcons(pyCharmCommunityApps, "pycharm.icns"))
	result = appendCandidateWithIcons(result, CursorID, "Cursor", cursor, bundleIcons(cursorApps, "Cursor.icns"))
	result = appendCandidateWithIcons(result, FileManagerID, "Finder", open, []string{"/System/Library/CoreServices/Finder.app/Contents/Resources/Finder.icns"})
	return result
}

func applicationPaths(roots, names []string) []string {
	result := make([]string, 0, len(roots)*len(names))
	for _, root := range roots {
		for _, name := range names {
			result = append(result, filepath.Join(root, name))
		}
	}
	return result
}

func bundleExecutables(applications []string, relativePaths ...string) []string {
	var result []string
	for _, application := range applications {
		for _, relativePath := range relativePaths {
			result = append(result, filepath.Join(application, filepath.FromSlash(relativePath)))
		}
	}
	return result
}

func bundleIcons(applications []string, preferredName string) []string {
	var result []string
	for _, application := range applications {
		resources := filepath.Join(application, "Contents", "Resources")
		result = append(result, filepath.Join(resources, preferredName))
		matches, _ := filepath.Glob(filepath.Join(resources, "*.icns"))
		slices.Sort(matches)
		result = append(result, matches...)
	}
	return slices.Compact(result)
}

func pyCharmDarwinApplications(roots []string, community bool) []string {
	var result []string
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "PyCharm*.app"))
		slices.Sort(matches)
		slices.Reverse(matches)
		for _, match := range matches {
			isCommunity := strings.Contains(strings.ToLower(match), "community") || strings.Contains(strings.ToLower(match), " ce")
			if isCommunity == community {
				result = append(result, match)
			}
		}
	}
	return result
}
