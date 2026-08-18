package domain

type PiExtensionOrigin string

const (
	PiExtensionOriginGlobal   PiExtensionOrigin = "global"
	PiExtensionOriginSettings PiExtensionOrigin = "settings"
	PiExtensionOriginPackage  PiExtensionOrigin = "package"
)

type PiExtensionSummary struct {
	Name   string            `json:"name"`
	Source string            `json:"source"`
	Path   string            `json:"path,omitempty"`
	Origin PiExtensionOrigin `json:"origin"`
}

type PiDeskTodoExtensionStatus struct {
	Path             string `json:"path"`
	Installed        bool   `json:"installed"`
	UpdateAvailable  bool   `json:"updateAvailable"`
	LegacyPath       string `json:"legacyPath,omitempty"`
	LegacyInstalled  bool   `json:"legacyInstalled"`
	LegacyBackupPath string `json:"legacyBackupPath,omitempty"`
}

type PiExtensionSnapshot struct {
	GlobalDirectory string                    `json:"globalDirectory"`
	SettingsPath    string                    `json:"settingsPath"`
	Extensions      []PiExtensionSummary      `json:"extensions"`
	Todo            PiDeskTodoExtensionStatus `json:"todo"`
}

type PiDeskTodoInstallResult struct {
	Todo           PiDeskTodoExtensionStatus `json:"todo"`
	ReplacedLegacy bool                      `json:"replacedLegacy"`
}
