package workspaceapp

import (
	"bytes"
	"embed"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Official brand assets and reviewed platform file-manager fallbacks are kept
// with their source and licence notes in assets/README.md.
//
//go:embed assets/*.png assets/*.ico
var iconAssets embed.FS

func fallbackIcon(applicationID string) (image.Image, error) {
	asset := ""
	switch applicationID {
	case VSCodeID:
		asset = "assets/vscode.ico"
	case CursorID:
		asset = "assets/cursor.ico"
	case PyCharmID, PyCharmCommunityID:
		asset = "assets/pycharm.png"
	case FileManagerID:
		asset = "assets/file-manager-" + runtime.GOOS + ".png"
	default:
		return nil, errors.New("no reviewed fallback icon")
	}
	data, err := iconAssets.ReadFile(asset)
	if err != nil {
		return nil, err
	}
	return decodeIconData(data, filepath.Ext(asset))
}

func decodeIconFile(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeIconData(data, filepath.Ext(path))
}

func decodeIconData(data []byte, extension string) (image.Image, error) {
	if strings.EqualFold(extension, ".ico") || isICOData(data) {
		return decodeICO(data)
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode icon image: %w", err)
	}
	return decoded, nil
}

func isICOData(data []byte) bool {
	return len(data) >= 6 && binary.LittleEndian.Uint16(data[:2]) == 0 && binary.LittleEndian.Uint16(data[2:4]) == 1
}

type icoEntry struct {
	width  int
	height int
	offset uint32
	size   uint32
}

func decodeICO(data []byte) (image.Image, error) {
	if !isICOData(data) {
		return nil, errors.New("invalid ICO header")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count <= 0 || count > 256 || 6+count*16 > len(data) {
		return nil, errors.New("invalid ICO directory")
	}
	entries := make([]icoEntry, 0, count)
	for index := 0; index < count; index++ {
		offset := 6 + index*16
		width, height := int(data[offset]), int(data[offset+1])
		if width == 0 {
			width = 256
		}
		if height == 0 {
			height = 256
		}
		size := binary.LittleEndian.Uint32(data[offset+8 : offset+12])
		imageOffset := binary.LittleEndian.Uint32(data[offset+12 : offset+16])
		if size == 0 || uint64(imageOffset)+uint64(size) > uint64(len(data)) {
			continue
		}
		entries = append(entries, icoEntry{width: width, height: height, offset: imageOffset, size: size})
	}
	sort.SliceStable(entries, func(left, right int) bool {
		return entries[left].width*entries[left].height > entries[right].width*entries[right].height
	})
	for _, entry := range entries {
		payload := data[entry.offset : entry.offset+entry.size]
		decoded, _, err := image.Decode(bytes.NewReader(payload))
		if err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("ICO contains no supported PNG image")
}
