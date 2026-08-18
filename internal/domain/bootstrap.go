package domain

import "time"

type RuntimeState string

const (
	RuntimeChecking RuntimeState = "checking"
	RuntimeReady    RuntimeState = "ready"
	RuntimeMissing  RuntimeState = "missing"
	RuntimeError    RuntimeState = "error"
)

type PiRuntimeStatus struct {
	State   RuntimeState `json:"state"`
	Command string       `json:"command,omitempty"`
	Version string       `json:"version,omitempty"`
	Message string       `json:"message,omitempty"`
}

type BootstrapState struct {
	ProductName      string          `json:"productName"`
	AppVersion       string          `json:"appVersion"`
	WailsVersion     string          `json:"wailsVersion"`
	WorkingDirectory string          `json:"workingDirectory"`
	Runtime          PiRuntimeStatus `json:"runtime"`
	Window           WindowState     `json:"window"`
}

type WindowState struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximized bool `json:"maximized"`
	Valid     bool `json:"valid"`
}

type UpdateCheckResult struct {
	Status         string    `json:"status"`
	CurrentVersion string    `json:"currentVersion"`
	LatestVersion  string    `json:"latestVersion,omitempty"`
	URL            string    `json:"url,omitempty"`
	Notes          string    `json:"notes,omitempty"`
	CheckedAt      time.Time `json:"checkedAt"`
	Message        string    `json:"message"`
}

// PiMaintenanceAction is deliberately an enum rather than a shell command.
// The desktop can maintain the Pi CLI without ever accepting executable text
// from the frontend.
type PiMaintenanceAction string

const (
	PiInstall    PiMaintenanceAction = "install"
	PiUpdateSelf PiMaintenanceAction = "update-self"
)

type PiMaintenanceRequest struct {
	Action PiMaintenanceAction `json:"action"`
}

type PiMaintenanceResult struct {
	Action  PiMaintenanceAction `json:"action"`
	Command string              `json:"command,omitempty"`
	Output  string              `json:"output,omitempty"`
	Runtime PiRuntimeStatus     `json:"runtime"`
}
