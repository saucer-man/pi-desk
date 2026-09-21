package domain

type BrowserStatus struct {
	TabID        string `json:"tabId"`
	ThreadID     string `json:"threadId"`
	Temporary    bool   `json:"temporary"`
	Loading      bool   `json:"loading"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	Error        string `json:"error,omitempty"`
	Attached     bool   `json:"attached"`
	URL          string `json:"url,omitempty"`
	Title        string `json:"title,omitempty"`
	ProfileDir   string `json:"profileDir,omitempty"`
}

type BrowserStartRequest struct {
	TabID    string `json:"tabId"`
	ThreadID string `json:"threadId"`
	URL      string `json:"url"`
}

type BrowserBoundsRequest struct {
	TabID   string `json:"tabId"`
	X       int32  `json:"x"`
	Y       int32  `json:"y"`
	Width   int32  `json:"width"`
	Height  int32  `json:"height"`
	Visible bool   `json:"visible"`
}

type BrowserOpenURLRequest struct {
	TabID string `json:"tabId,omitempty"`
	URL   string `json:"url"`
}

type BrowserClickRequest struct {
	TabID      string  `json:"tabId,omitempty"`
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	Button     string  `json:"button,omitempty"`
	ClickCount int     `json:"clickCount,omitempty"`
}

type BrowserWheelRequest struct {
	TabID  string  `json:"tabId,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	DeltaX float64 `json:"deltaX"`
	DeltaY float64 `json:"deltaY"`
}

type BrowserKeyRequest struct {
	TabID     string `json:"tabId,omitempty"`
	Key       string `json:"key"`
	Modifiers int    `json:"modifiers,omitempty"`
}

type BrowserTextInputRequest struct {
	TabID string `json:"tabId,omitempty"`
	Text  string `json:"text"`
}

type BrowserEvent struct {
	Status    *BrowserStatus `json:"status,omitempty"`
	ThreadID  string         `json:"threadId,omitempty"`
	TabID     string         `json:"tabId,omitempty"`
	Type      string         `json:"type"`
	Sequence  uint64         `json:"sequence"`
	DataB64   string         `json:"dataB64,omitempty"`
	CssWidth  float64        `json:"cssWidth,omitempty"`
	CssHeight float64        `json:"cssHeight,omitempty"`
	URL       string         `json:"url,omitempty"`
	Title     string         `json:"title,omitempty"`
	Error     string         `json:"error,omitempty"`
}
