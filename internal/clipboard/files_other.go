//go:build !windows

package clipboard

// Native file-list paste currently targets Windows Explorer. Browser image and
// text paste remain available on the other desktop platforms.
func FilePaths() ([]string, error) { return nil, nil }
