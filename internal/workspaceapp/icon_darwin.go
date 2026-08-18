//go:build darwin

package workspaceapp

import (
	"context"
	"errors"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func loadNativeIcon(item candidate) (image.Image, error) {
	var lastError error
	for _, path := range item.iconPaths {
		if strings.EqualFold(filepath.Ext(path), ".icns") {
			icon, err := convertICNSToImage(path)
			if err == nil {
				return icon, nil
			}
			lastError = err
			continue
		}
		icon, err := decodeIconFile(path)
		if err == nil {
			return icon, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = errors.New("application bundle has no icon")
	}
	return nil, lastError
}

func convertICNSToImage(path string) (image.Image, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp("", "pi-desk-workspace-icon-*.png")
	if err != nil {
		return nil, err
	}
	outputPath := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(outputPath)
		return nil, err
	}
	defer os.Remove(outputPath)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/usr/bin/sips", "-s", "format", "png", "--resampleWidth", "256", path, "--out", outputPath)
	if output, err := command.CombinedOutput(); err != nil {
		return nil, errors.New("convert application ICNS: " + strings.TrimSpace(string(output)))
	}
	return decodeIconFile(outputPath)
}
