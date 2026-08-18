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
}

type UpsertModelConfigRequest struct {
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
	ThinkingLevelMapJSON string            `json:"thinkingLevelMapJson,omitempty"`
	ModelCompatJSON      string            `json:"modelCompatJson,omitempty"`
}

type AddModelsConfigRequest struct {
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

type TestModelConfigRequest struct {
	BaseURL string            `json:"baseUrl"`
	API     string            `json:"api"`
	APIKey  string            `json:"apiKey,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	ModelID string            `json:"modelId"`
	Prompt  string            `json:"prompt"`
}

type ModelTestResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latencyMs"`
	Status    int    `json:"status,omitempty"`
	Response  string `json:"response,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ModelQuotaRequest struct {
	BaseURL string            `json:"baseUrl"`
	API     string            `json:"api"`
	APIKey  string            `json:"apiKey,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
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
	BaseURL string            `json:"baseUrl"`
	API     string            `json:"api"`
	APIKey  string            `json:"apiKey,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
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
