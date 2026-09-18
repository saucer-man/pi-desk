package domain

type BrowserStatus struct {
	Attached   bool   `json:"attached"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	ProfileDir string `json:"profileDir,omitempty"`
}

type BrowserOpenURLRequest struct {
	URL string `json:"url"`
}

type BrowserClickRequest struct {
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
}

type BrowserWheelRequest struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	DeltaX float64 `json:"deltaX"`
	DeltaY float64 `json:"deltaY"`
}

type BrowserKeyRequest struct {
	Key       string `json:"key"`
	Modifiers int    `json:"modifiers,omitempty"`
}

type BrowserTextInputRequest struct {
	Text string `json:"text"`
}

type BrowserEvent struct {
	Type      string `json:"type"`
	Sequence  uint64 `json:"sequence"`
	DataB64   string `json:"dataB64,omitempty"`
	CssWidth  int    `json:"cssWidth,omitempty"`
	CssHeight int    `json:"cssHeight,omitempty"`
	URL       string `json:"url,omitempty"`
	Title     string `json:"title,omitempty"`
	Error     string `json:"error,omitempty"`
}
