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

type PiPackageScope string

const (
	PiPackageScopeGlobal  PiPackageScope = "global"
	PiPackageScopeProject PiPackageScope = "project"
)

type PiPackageSummary struct {
	Source  string         `json:"source"`
	Scope   PiPackageScope `json:"scope"`
	Enabled bool           `json:"enabled"`
}

type ListPiPackagesRequest struct {
	WorkspacePath string `json:"workspacePath,omitempty"`
}

type PiPackageSnapshot struct {
	GlobalSettingsPath  string             `json:"globalSettingsPath"`
	ProjectSettingsPath string             `json:"projectSettingsPath,omitempty"`
	ProjectEnabled      bool               `json:"projectEnabled"`
	ProjectNotice       string             `json:"projectNotice,omitempty"`
	Packages            []PiPackageSummary `json:"packages"`
}

type PiPackageRequest struct {
	Source        string         `json:"source"`
	Scope         PiPackageScope `json:"scope"`
	WorkspacePath string         `json:"workspacePath,omitempty"`
}

type SetPiPackageEnabledRequest struct {
	PiPackageRequest
	Enabled bool `json:"enabled"`
}

type PiPackageCommandResult struct {
	Output string `json:"output,omitempty"`
}
