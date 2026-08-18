//go:build linux

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
		if strings.EqualFold(filepath.Ext(path), ".svg") {
			icon, err := convertSVGToImage(path)
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
		lastError = errors.New("desktop entry has no readable icon")
	}
	return nil, lastError
}

func convertSVGToImage(path string) (image.Image, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	rasterizer, err := exec.LookPath("rsvg-convert")
	if err != nil || !filepath.IsAbs(rasterizer) {
		return nil, errors.New("rsvg-convert is unavailable")
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
	command := exec.CommandContext(ctx, rasterizer, "--width", "256", "--height", "256", "--keep-aspect-ratio", "--output", outputPath, path)
	if output, err := command.CombinedOutput(); err != nil {
		return nil, errors.New("convert desktop SVG icon: " + strings.TrimSpace(string(output)))
	}
	return decodeIconFile(outputPath)
}
