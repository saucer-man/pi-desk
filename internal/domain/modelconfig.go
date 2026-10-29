package domain

type ModelConfigSnapshot struct {
	Path      string                 `json:"path"`
	Providers []ManagedModelProvider `json:"providers"`
}

type ManagedModelProvider struct {
	ID         string            `json:"id"`
	BaseURL    string            `json:"baseUrl,omitempty"`
	API        string            `json:"api,omitempty"`
	APIKey     string            `json:"apiKey,omitempty"`
	HasAPIKey  bool              `json:"hasApiKey,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	CompatJSON string            `json:"compatJson,omitempty"`
	Models     []ManagedModel    `json:"models"`
}

type ManagedModel struct {
	ID                   string `json:"id"`
	Name                 string `json:"name,omitempty"`
	ContextWindow        int    `json:"contextWindow"`
	MaxTokens            int    `json:"maxTokens"`
	Reasoning            bool   `json:"reasoning"`
	ImageInput           bool   `json:"imageInput"`
	ReserveTokens        *int   `json:"reserveTokens,omitempty"`
	KeepRecentTokens     *int   `json:"keepRecentTokens,omitempty"`
	ImageMaxWidth        *int   `json:"imageMaxWidth,omitempty"`
	ImageMaxHeight       *int   `json:"imageMaxHeight,omitempty"`
	ImageMaxBytes        *int   `json:"imageMaxBytes,omitempty"`
	ImageJPEGQuality     *int   `json:"imageJpegQuality,omitempty"`
	ThinkingLevelMapJSON string `json:"thinkingLevelMapJson,omitempty"`
	CompatJSON           string `json:"compatJson,omitempty"`
}

// SelectableModel is the credential-free subset exposed outside model management.
type SelectableModel struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Provider      string `json:"provider"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	Reasoning     bool   `json:"reasoning,omitempty"`
	Source        string `json:"source,omitempty"`
}

type ModelAuthProvider struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	OAuth             bool   `json:"oauth"`
	APIKey            bool   `json:"apiKey"`
	Configured        bool   `json:"configured"`
	Stored            bool   `json:"stored"`
	AuthType          string `json:"authType"`
	AuthSource        string `json:"authSource"`
	Warning           string `json:"warning,omitempty"`
	CatalogRestricted bool   `json:"catalogRestricted,omitempty"`
}

type ModelCatalogSnapshot struct {
	AgentDir        string              `json:"agentDir"`
	Providers       []ModelAuthProvider `json:"providers"`
	Models          []SelectableModel   `json:"models"`
	DefaultProvider string              `json:"defaultProvider"`
	DefaultModel    string              `json:"defaultModel"`
	Warning         string              `json:"warning,omitempty"`
}

type ModelAuthRequest struct {
	OperationID string `json:"operationId,omitempty"`
	ProviderID  string `json:"providerId"`
	APIKey      string `json:"apiKey,omitempty"`
}

type ModelAuthReply struct {
	OperationID string `json:"operationId"`
	PromptID    int    `json:"promptId"`
	Value       string `json:"value"`
}

type ModelAuthEvent struct {
	OperationID     string            `json:"operationId"`
	Type            string            `json:"type"`
	URL             string            `json:"url,omitempty"`
	Message         string            `json:"message,omitempty"`
	Instructions    string            `json:"instructions,omitempty"`
	PromptID        int               `json:"promptId,omitempty"`
	PromptType      string            `json:"promptType,omitempty"`
	Placeholder     string            `json:"placeholder,omitempty"`
	UserCode        string            `json:"userCode,omitempty"`
	VerificationURI string            `json:"verificationUri,omitempty"`
	Options         []ModelAuthOption `json:"options,omitempty"`
}

type ModelAuthOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type DefaultModelRequest struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

type UpsertModelConfigRequest struct {
	ClearAPIKey          bool              `json:"clearApiKey,omitempty"`
	OriginalProviderID   string            `json:"originalProviderId,omitempty"`
	OriginalModelID      string            `json:"originalModelId,omitempty"`
	ProviderID           string            `json:"providerId"`
	BaseURL              string            `json:"baseUrl,omitempty"`
	API                  string            `json:"api,omitempty"`
	APIKey               string            `json:"apiKey,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	ProviderCompatJSON   string            `json:"providerCompatJson,omitempty"`
	ModelID              string            `json:"modelId"`
	ModelName            string            `json:"modelName,omitempty"`
	ContextWindow        int               `json:"contextWindow"`
	MaxTokens            int               `json:"maxTokens"`
	Reasoning            bool              `json:"reasoning"`
	ImageInput           bool              `json:"imageInput"`
	ReserveTokens        *int              `json:"reserveTokens,omitempty"`
	KeepRecentTokens     *int              `json:"keepRecentTokens,omitempty"`
	ImageMaxWidth        *int              `json:"imageMaxWidth,omitempty"`
	ImageMaxHeight       *int              `json:"imageMaxHeight,omitempty"`
	ImageMaxBytes        *int              `json:"imageMaxBytes,omitempty"`
	ImageJPEGQuality     *int              `json:"imageJpegQuality,omitempty"`
	ThinkingLevelMapJSON string            `json:"thinkingLevelMapJson,omitempty"`
	ModelCompatJSON      string            `json:"modelCompatJson,omitempty"`
}

type AddModelsConfigRequest struct {
	ClearAPIKey        bool              `json:"clearApiKey,omitempty"`
	OriginalProviderID string            `json:"originalProviderId,omitempty"`
	ProviderID         string            `json:"providerId"`
	BaseURL            string            `json:"baseUrl,omitempty"`
	API                string            `json:"api,omitempty"`
	APIKey             string            `json:"apiKey,omitempty"`
	Headers            map[string]string `json:"headers,omitempty"`
	ProviderCompatJSON string            `json:"providerCompatJson,omitempty"`
	Models             []ManagedModel    `json:"models"`
}

type DeleteModelConfigRequest struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

type DeleteProviderConfigRequest struct {
	ProviderID string `json:"providerId"`
}

type TestModelConfigRequest struct {
	CredentialProviderID string            `json:"credentialProviderId,omitempty"`
	ProviderID           string            `json:"providerId,omitempty"`
	BaseURL              string            `json:"baseUrl"`
	API                  string            `json:"api"`
	APIKey               string            `json:"apiKey,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	ModelID              string            `json:"modelId"`
	Prompt               string            `json:"prompt"`
}

type ModelTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latencyMs"`
	Status    int    `json:"status,omitempty"`
	Response  string `json:"response,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ModelQuotaRequest struct {
	CredentialProviderID string            `json:"credentialProviderId,omitempty"`
	BaseURL              string            `json:"baseUrl"`
	API                  string            `json:"api"`
	APIKey               string            `json:"apiKey,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
}

type ModelQuotaResult struct {
	OK          bool   `json:"ok"`
	LatencyMS   int64  `json:"latencyMs"`
	Status      int    `json:"status,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	Summary     string `json:"summary,omitempty"`
	DetailsJSON string `json:"detailsJson,omitempty"`
	Error       string `json:"error,omitempty"`
}

type DiscoverModelsRequest struct {
	CredentialProviderID string            `json:"credentialProviderId,omitempty"`
	BaseURL              string            `json:"baseUrl"`
	API                  string            `json:"api"`
	APIKey               string            `json:"apiKey,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
}

type DiscoveredModel struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	MaxTokens     int    `json:"maxTokens,omitempty"`
	Reasoning     bool   `json:"reasoning,omitempty"`
	ImageInput    bool   `json:"imageInput,omitempty"`
}

type ModelDiscoveryResult struct {
	Models   []DiscoveredModel `json:"models"`
	Endpoint string            `json:"endpoint"`
}
