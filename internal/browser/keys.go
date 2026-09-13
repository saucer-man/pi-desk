package browser

import (
	"fmt"
	"strings"
)

// KeySpec is the CDP input shape for one key press. Panel input sends the
// base key name plus a modifier bitmask (alt=1, ctrl=2, meta=4, shift=8);
// chord parsing stays on the callers that own the chord syntax.
type KeySpec struct {
	Key        string
	Code       string
	VirtualKey int
	Text       string
}

var namedKeys = map[string]KeySpec{
	"enter":      {"Enter", "Enter", 13, "\r"},
	"backspace":  {"Backspace", "Backspace", 8, ""},
	"tab":        {"Tab", "Tab", 9, "\t"},
	"escape":     {"Escape", "Escape", 27, ""},
	"esc":        {"Escape", "Escape", 27, ""},
	"space":      {" ", "Space", 32, " "},
	"pageup":     {"PageUp", "PageUp", 33, ""},
	"pagedown":   {"PageDown", "PageDown", 34, ""},
	"end":        {"End", "End", 35, ""},
	"home":       {"Home", "Home", 36, ""},
	"arrowleft":  {"ArrowLeft", "ArrowLeft", 37, ""},
	"arrowup":    {"ArrowUp", "ArrowUp", 38, ""},
	"arrowright": {"ArrowRight", "ArrowRight", 39, ""},
	"arrowdown":  {"ArrowDown", "ArrowDown", 40, ""},
	"delete":     {"Delete", "Delete", 46, ""},
	"insert":     {"Insert", "Insert", 45, ""},
}

// ParseKeySpec resolves a single key name into CDP input fields.
func ParseKeySpec(key string) (KeySpec, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" || len(trimmed) > 32 {
		return KeySpec{}, fmt.Errorf("unsupported key %q", key)
	}
	lower := strings.ToLower(trimmed)
	if named, exists := namedKeys[lower]; exists {
		return named, nil
	}
	if function, exists := functionKey(lower); exists {
		return function, nil
	}
	characters := []rune(trimmed)
	if len(characters) == 1 {
		character := characters[0]
		switch {
		case character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z':
			upper := strings.ToUpper(trimmed)
			return KeySpec{Key: strings.ToLower(trimmed), Code: "Key" + upper, VirtualKey: int(upper[0]), Text: trimmed}, nil
		case character >= '0' && character <= '9':
			return KeySpec{Key: trimmed, Code: "Digit" + trimmed, VirtualKey: int(character), Text: trimmed}, nil
		default:
			return KeySpec{Key: trimmed, Code: trimmed, VirtualKey: int(character), Text: trimmed}, nil
		}
	}
	return KeySpec{}, fmt.Errorf("unsupported key %q", key)
}

func functionKey(lower string) (KeySpec, bool) {
	if len(lower) == 2 || len(lower) == 3 {
		if lower[0] == 'f' {
			index := 0
			for _, character := range lower[1:] {
				if character < '0' || character > '9' {
					return KeySpec{}, false
				}
				index = index*10 + int(character-'0')
			}
			if index >= 1 && index <= 12 {
				name := fmt.Sprintf("F%d", index)
				return KeySpec{Key: name, Code: name, VirtualKey: 111 + index}, true
			}
		}
	}
	return KeySpec{}, false
}
