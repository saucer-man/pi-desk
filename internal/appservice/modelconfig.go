package appservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"pi-desk/internal/domain"

	"github.com/natefinch/atomic"
)

const (
	maxModelsConfigBytes     = 4 << 20
	maxCompatJSONBytes       = 64 << 10
	maxCredentialBytes       = 64 << 10
	maxModelIdentifier       = 256
	maxBaseURLBytes          = 2048
	maxModelTokens           = 10_000_000
	maxProviderHeaders       = 100
	maxHeaderNameBytes       = 256
	maxHeaderValueBytes      = 64 << 10
	maxHeadersBytes          = 64 << 10
	defaultContextWindow     = 128_000
	defaultMaxTokens         = 16_384
	defaultProviderUserAgent = "codex_cli_rs/0.146.0 (Windows 11.0.26100; x86_64) Terminal"
)

var supportedModelAPIs = map[string]struct{}{
	"openai-completions":   {},
	"openai-responses":     {},
	"anthropic-messages":   {},
	"google-generative-ai": {},
}

var supportedThinkingLevels = map[string]struct{}{
	"off": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {},
}

type ModelConfigService struct {
	modelsPath string
	pathErr    error
	client     httpDoer

	mu sync.Mutex
}

func NewModelConfigService() *ModelConfigService {
	path, err := defaultModelsPath()
	return &ModelConfigService{modelsPath: path, pathErr: err, client: defaultModelHTTPClient()}
}

func newModelConfigService(path string, client httpDoer) *ModelConfigService {
	if client == nil {
		client = defaultModelHTTPClient()
	}
	return &ModelConfigService{modelsPath: path, client: client}
}

func defaultModelsPath() (string, error) {
	directory, err := defaultPiAgentDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, "models.json"), nil
}

func defaultPiAgentDirectory() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("PI_CODING_AGENT_DIR")); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolve Pi agent directory: %w", err)
		}
		return filepath.Clean(absolute), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home directory: %w", err)
	}
	return filepath.Join(home, ".pi", "agent"), nil
}

func (service *ModelConfigService) GetModelsConfig() (domain.ModelConfigSnapshot, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	root, err := service.readDocument()
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	return service.snapshot(root)
}

func (service *ModelConfigService) GetConfiguredModels() ([]domain.SelectableModel, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	root, err := service.readDocument()
	if err != nil {
		return nil, err
	}
	snapshot, err := service.snapshot(root)
	if err != nil {
		return nil, err
	}
	models := make([]domain.SelectableModel, 0)
	for _, provider := range snapshot.Providers {
		for _, model := range provider.Models {
			models = append(models, domain.SelectableModel{
				ID: model.ID, Name: model.Name, Provider: provider.ID,
				ContextWindow: model.ContextWindow, Reasoning: model.Reasoning,
			})
		}
	}
	return models, nil
}

func (service *ModelConfigService) UpsertModel(request domain.UpsertModelConfigRequest) (domain.ModelConfigSnapshot, error) {
	request = normalizeModelRequest(request)
	providerCompat, modelCompat, thinkingLevelMap, err := validateModelRequest(request)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	providerHeaders, err := normalizeProviderHeaders(request.Headers)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	root, err := service.readDocument()
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	providers, err := providersObject(root)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	if request.OriginalProviderID != "" && request.OriginalProviderID != request.ProviderID {
		provider, exists, providerErr := objectValue(providers, request.OriginalProviderID)
		if providerErr != nil {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", request.OriginalProviderID, providerErr)
		}
		if !exists {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s was not found", request.OriginalProviderID)
		}
		if _, targetExists := providers[request.ProviderID]; targetExists {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s already exists", request.ProviderID)
		}
		delete(providers, request.OriginalProviderID)
		providers[request.ProviderID] = provider
	}

	provider, exists, err := objectValue(providers, request.ProviderID)
	if err != nil {
		return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", request.ProviderID, err)
	}
	providerWasCreated := !exists
	if providerWasCreated {
		provider = map[string]any{}
		providers[request.ProviderID] = provider
	}
	setOptionalString(provider, "baseUrl", request.BaseURL)
	setOptionalString(provider, "api", request.API)
	setOptionalObject(provider, "compat", providerCompat)
	setOptionalString(provider, "apiKey", request.APIKey)
	if request.Headers != nil {
		setOptionalStringMap(provider, "headers", providerHeaders)
	} else if providerWasCreated {
		setOptionalStringMap(provider, "headers", map[string]string{"User-Agent": defaultProviderUserAgent})
	}

	models, err := modelObjects(provider)
	if err != nil {
		return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", request.ProviderID, err)
	}
	target := -1
	for index, model := range models {
		id, _ := model["id"].(string)
		if request.OriginalModelID != "" && id == request.OriginalModelID {
			target = index
			continue
		}
		if id == request.ModelID {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s/%s already exists", request.ProviderID, request.ModelID)
		}
	}
	var model map[string]any
	if target >= 0 {
		model = models[target]
	} else {
		if request.OriginalModelID != "" {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s/%s was not found", request.ProviderID, request.OriginalModelID)
		}
		model = map[string]any{}
		models = append(models, model)
		target = len(models) - 1
	}
	model["id"] = request.ModelID
	setOptionalString(model, "name", request.ModelName)
	model["contextWindow"] = request.ContextWindow
	model["maxTokens"] = request.MaxTokens
	model["reasoning"] = request.Reasoning
	if request.ImageInput {
		model["input"] = []any{"text", "image"}
	} else {
		model["input"] = []any{"text"}
	}
	setOptionalObject(model, "thinkingLevelMap", thinkingLevelMap)
	setOptionalObject(model, "compat", modelCompat)
	models[target] = model
	provider["models"] = objectSlice(models)

	if err := service.writeDocument(root); err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	return service.snapshot(root)
}

// AddModels writes a discovered model set in one atomic models.json update.
// Existing IDs are rejected so the caller cannot silently overwrite manual settings.
func (service *ModelConfigService) AddModels(request domain.AddModelsConfigRequest) (domain.ModelConfigSnapshot, error) {
	request.OriginalProviderID = strings.TrimSpace(request.OriginalProviderID)
	request.ProviderID = strings.TrimSpace(request.ProviderID)
	request.BaseURL = strings.TrimSpace(request.BaseURL)
	request.API = strings.TrimSpace(request.API)
	request.APIKey = strings.TrimSpace(request.APIKey)
	request.ProviderCompatJSON = strings.TrimSpace(request.ProviderCompatJSON)
	if err := validateIdentifier("provider id", request.ProviderID); err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	if request.OriginalProviderID != "" {
		if err := validateIdentifier("original provider id", request.OriginalProviderID); err != nil {
			return domain.ModelConfigSnapshot{}, err
		}
	}
	if err := validateBaseURL(request.BaseURL); err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	if request.API != "" {
		if _, ok := supportedModelAPIs[request.API]; !ok {
			return domain.ModelConfigSnapshot{}, errors.New("unsupported model API type")
		}
	}
	if len(request.APIKey) > maxCredentialBytes || strings.ContainsAny(request.APIKey, "\r\n") {
		return domain.ModelConfigSnapshot{}, errors.New("API key is invalid")
	}
	providerCompat, err := parseOptionalObject("provider compatibility", request.ProviderCompatJSON)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	providerHeaders, err := normalizeProviderHeaders(request.Headers)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	if len(request.Models) == 0 {
		return domain.ModelConfigSnapshot{}, errors.New("at least one model is required")
	}
	if len(request.Models) > maxDiscoveredModels {
		return domain.ModelConfigSnapshot{}, fmt.Errorf("cannot add more than %d models at once", maxDiscoveredModels)
	}
	modelCompat := make([]map[string]any, len(request.Models))
	thinkingLevelMaps := make([]map[string]any, len(request.Models))
	for index, model := range request.Models {
		if err := validateIdentifier("model id", strings.TrimSpace(model.ID)); err != nil {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %d: %w", index+1, err)
		}
		if len(model.Name) > maxModelIdentifier {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s name exceeds %d characters", model.ID, maxModelIdentifier)
		}
		if model.ContextWindow < 1 || model.ContextWindow > maxModelTokens {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s context window must be between 1 and %d", model.ID, maxModelTokens)
		}
		if model.MaxTokens < 1 || model.MaxTokens > model.ContextWindow {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s max tokens must be positive and no larger than the context window", model.ID)
		}
		modelCompat[index], err = parseOptionalObject("model compatibility", strings.TrimSpace(model.CompatJSON))
		if err != nil {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s: %w", model.ID, err)
		}
		thinkingLevelMaps[index], err = parseThinkingLevelMap(model.ThinkingLevelMapJSON)
		if err != nil {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s: %w", model.ID, err)
		}
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	root, err := service.readDocument()
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	providers, err := providersObject(root)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	if request.OriginalProviderID != "" && request.OriginalProviderID != request.ProviderID {
		provider, exists, providerErr := objectValue(providers, request.OriginalProviderID)
		if providerErr != nil {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", request.OriginalProviderID, providerErr)
		}
		if !exists {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s was not found", request.OriginalProviderID)
		}
		if _, targetExists := providers[request.ProviderID]; targetExists {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s already exists", request.ProviderID)
		}
		delete(providers, request.OriginalProviderID)
		providers[request.ProviderID] = provider
	}
	provider, exists, err := objectValue(providers, request.ProviderID)
	if err != nil {
		return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", request.ProviderID, err)
	}
	providerWasCreated := !exists
	if providerWasCreated {
		provider = map[string]any{}
		providers[request.ProviderID] = provider
	}
	setOptionalString(provider, "baseUrl", request.BaseURL)
	setOptionalString(provider, "api", request.API)
	setOptionalObject(provider, "compat", providerCompat)
	setOptionalString(provider, "apiKey", request.APIKey)
	if request.Headers != nil {
		setOptionalStringMap(provider, "headers", providerHeaders)
	} else if providerWasCreated {
		setOptionalStringMap(provider, "headers", map[string]string{"User-Agent": defaultProviderUserAgent})
	}
	models, err := modelObjects(provider)
	if err != nil {
		return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", request.ProviderID, err)
	}
	existing := make(map[string]struct{}, len(models))
	for _, model := range models {
		if id := stringValue(model["id"]); id != "" {
			existing[id] = struct{}{}
		}
	}
	for index, model := range request.Models {
		modelID := strings.TrimSpace(model.ID)
		if _, found := existing[modelID]; found {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s/%s already exists", request.ProviderID, modelID)
		}
		modelObject := map[string]any{
			"id":            modelID,
			"contextWindow": model.ContextWindow,
			"maxTokens":     model.MaxTokens,
			"reasoning":     model.Reasoning,
		}
		setOptionalString(modelObject, "name", strings.TrimSpace(model.Name))
		if model.ImageInput {
			modelObject["input"] = []any{"text", "image"}
		} else {
			modelObject["input"] = []any{"text"}
		}
		setOptionalObject(modelObject, "thinkingLevelMap", thinkingLevelMaps[index])
		setOptionalObject(modelObject, "compat", modelCompat[index])
		models = append(models, modelObject)
		existing[modelID] = struct{}{}
	}
	provider["models"] = objectSlice(models)
	if err := service.writeDocument(root); err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	return service.snapshot(root)
}

func (service *ModelConfigService) DeleteModel(request domain.DeleteModelConfigRequest) (domain.ModelConfigSnapshot, error) {
	request.ProviderID = strings.TrimSpace(request.ProviderID)
	request.ModelID = strings.TrimSpace(request.ModelID)
	if err := validateIdentifier("provider id", request.ProviderID); err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	if err := validateIdentifier("model id", request.ModelID); err != nil {
		return domain.ModelConfigSnapshot{}, err
	}

	service.mu.Lock()
	defer service.mu.Unlock()
	root, err := service.readDocument()
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	providers, err := providersObject(root)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	provider, exists, err := objectValue(providers, request.ProviderID)
	if err != nil || !exists {
		return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s was not found", request.ProviderID)
	}
	models, err := modelObjects(provider)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	filtered := make([]map[string]any, 0, len(models))
	found := false
	for _, model := range models {
		if id, _ := model["id"].(string); id == request.ModelID {
			found = true
			continue
		}
		filtered = append(filtered, model)
	}
	if !found {
		return domain.ModelConfigSnapshot{}, fmt.Errorf("model %s/%s was not found", request.ProviderID, request.ModelID)
	}
	provider["models"] = objectSlice(filtered)
	if err := service.writeDocument(root); err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	return service.snapshot(root)
}

func normalizeModelRequest(request domain.UpsertModelConfigRequest) domain.UpsertModelConfigRequest {
	request.OriginalProviderID = strings.TrimSpace(request.OriginalProviderID)
	request.OriginalModelID = strings.TrimSpace(request.OriginalModelID)
	request.ProviderID = strings.TrimSpace(request.ProviderID)
	request.BaseURL = strings.TrimSpace(request.BaseURL)
	request.API = strings.TrimSpace(request.API)
	request.APIKey = strings.TrimSpace(request.APIKey)
	request.ProviderCompatJSON = strings.TrimSpace(request.ProviderCompatJSON)
	request.ModelID = strings.TrimSpace(request.ModelID)
	request.ModelName = strings.TrimSpace(request.ModelName)
	request.ThinkingLevelMapJSON = strings.TrimSpace(request.ThinkingLevelMapJSON)
	request.ModelCompatJSON = strings.TrimSpace(request.ModelCompatJSON)
	return request
}

func validateModelRequest(request domain.UpsertModelConfigRequest) (map[string]any, map[string]any, map[string]any, error) {
	if err := validateIdentifier("provider id", request.ProviderID); err != nil {
		return nil, nil, nil, err
	}
	if request.OriginalProviderID != "" {
		if err := validateIdentifier("original provider id", request.OriginalProviderID); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := validateIdentifier("model id", request.ModelID); err != nil {
		return nil, nil, nil, err
	}
	if request.OriginalModelID != "" {
		if err := validateIdentifier("original model id", request.OriginalModelID); err != nil {
			return nil, nil, nil, err
		}
	}
	if len(request.ModelName) > maxModelIdentifier {
		return nil, nil, nil, fmt.Errorf("model name exceeds %d characters", maxModelIdentifier)
	}
	if request.API != "" {
		if _, ok := supportedModelAPIs[request.API]; !ok {
			return nil, nil, nil, errors.New("unsupported model API type")
		}
	}
	if err := validateBaseURL(request.BaseURL); err != nil {
		return nil, nil, nil, err
	}
	if request.ContextWindow < 1 || request.ContextWindow > maxModelTokens {
		return nil, nil, nil, fmt.Errorf("context window must be between 1 and %d", maxModelTokens)
	}
	if request.MaxTokens < 1 || request.MaxTokens > request.ContextWindow {
		return nil, nil, nil, errors.New("max tokens must be positive and no larger than the context window")
	}
	if len(request.APIKey) > maxCredentialBytes || strings.ContainsAny(request.APIKey, "\r\n") {
		return nil, nil, nil, errors.New("API key is invalid")
	}
	providerCompat, err := parseOptionalObject("provider compatibility", request.ProviderCompatJSON)
	if err != nil {
		return nil, nil, nil, err
	}
	modelCompat, err := parseOptionalObject("model compatibility", request.ModelCompatJSON)
	if err != nil {
		return nil, nil, nil, err
	}
	thinkingLevelMap, err := parseThinkingLevelMap(request.ThinkingLevelMapJSON)
	if err != nil {
		return nil, nil, nil, err
	}
	return providerCompat, modelCompat, thinkingLevelMap, nil
}

func validateIdentifier(label, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if len(value) > maxModelIdentifier || strings.ContainsAny(value, "\r\n\t ") {
		return fmt.Errorf("%s is invalid", label)
	}
	return nil
}

func validateBaseURL(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxBaseURLBytes {
		return errors.New("base URL exceeds 2048 bytes")
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("base URL must be an absolute HTTP or HTTPS URL")
	}
	return nil
}

func parseOptionalObject(label, value string) (map[string]any, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > maxCompatJSONBytes {
		return nil, fmt.Errorf("%s exceeds 64 KiB", label)
	}
	var result map[string]any
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil || result == nil {
		return nil, fmt.Errorf("%s must be a JSON object", label)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, fmt.Errorf("%s must contain exactly one JSON object", label)
	}
	return result, nil
}

func parseThinkingLevelMap(value string) (map[string]any, error) {
	result, err := parseOptionalObject("thinking level map", strings.TrimSpace(value))
	if err != nil || result == nil {
		return result, err
	}
	for level, mapped := range result {
		if _, ok := supportedThinkingLevels[level]; !ok {
			return nil, fmt.Errorf("thinking level map contains unsupported level %q", level)
		}
		if mapped == nil {
			continue
		}
		text, ok := mapped.(string)
		if !ok || strings.TrimSpace(text) == "" || len(text) > maxModelIdentifier || strings.ContainsAny(text, "\r\n") {
			return nil, fmt.Errorf("thinking level map value for %q must be a non-empty string or null", level)
		}
	}
	return result, nil
}

func (service *ModelConfigService) readDocument() (map[string]any, error) {
	if service.pathErr != nil {
		return nil, service.pathErr
	}
	data, err := os.ReadFile(service.modelsPath)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{"providers": map[string]any{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read models.json: %w", err)
	}
	if len(data) > maxModelsConfigBytes {
		return nil, errors.New("models.json exceeds the 4 MiB safety limit")
	}
	root := map[string]any{}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("decode models.json: %w", err)
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, fmt.Errorf("decode models.json: %w", err)
	}
	if _, err := providersObject(root); err != nil {
		return nil, err
	}
	return root, nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("unexpected trailing JSON value")
	}
	return err
}

func providersObject(root map[string]any) (map[string]any, error) {
	value, exists := root["providers"]
	if !exists {
		providers := map[string]any{}
		root["providers"] = providers
		return providers, nil
	}
	providers, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("models.json providers must be an object")
	}
	return providers, nil
}

func objectValue(values map[string]any, key string) (map[string]any, bool, error) {
	value, exists := values[key]
	if !exists {
		return nil, false, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false, errors.New("configuration must be an object")
	}
	return object, true, nil
}

func modelObjects(provider map[string]any) ([]map[string]any, error) {
	value, exists := provider["models"]
	if !exists {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("models must be an array")
	}
	models := make([]map[string]any, 0, len(items))
	for _, item := range items {
		model, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("each model must be an object")
		}
		models = append(models, model)
	}
	return models, nil
}

func objectSlice(values []map[string]any) []any {
	result := make([]any, len(values))
	for index := range values {
		result[index] = values[index]
	}
	return result
}

func setOptionalString(object map[string]any, key, value string) {
	if value == "" {
		delete(object, key)
		return
	}
	object[key] = value
}

func setOptionalObject(object map[string]any, key string, value map[string]any) {
	if value == nil {
		delete(object, key)
		return
	}
	object[key] = value
}

func setOptionalStringMap(object map[string]any, key string, value map[string]string) {
	if len(value) == 0 {
		delete(object, key)
		return
	}
	object[key] = value
}

func normalizeProviderHeaders(headers map[string]string) (map[string]string, error) {
	if headers == nil {
		return nil, nil
	}
	if len(headers) > maxProviderHeaders {
		return nil, fmt.Errorf("provider headers cannot contain more than %d entries", maxProviderHeaders)
	}
	result := make(map[string]string, len(headers))
	seen := make(map[string]struct{}, len(headers))
	totalBytes := 0
	for name, value := range headers {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("provider header name is required")
		}
		if len(name) > maxHeaderNameBytes || !validHTTPHeaderName(name) {
			return nil, fmt.Errorf("provider header %q has an invalid name", name)
		}
		if len(value) > maxHeaderValueBytes || !validHTTPHeaderValue(value) {
			return nil, fmt.Errorf("provider header %q has an invalid value", name)
		}
		lowerName := strings.ToLower(name)
		if _, exists := seen[lowerName]; exists {
			return nil, fmt.Errorf("provider header %q is duplicated", name)
		}
		seen[lowerName] = struct{}{}
		totalBytes += len(name) + len(value)
		if totalBytes > maxHeadersBytes {
			return nil, fmt.Errorf("provider headers exceed %d KiB", maxHeadersBytes>>10)
		}
		result[name] = value
	}
	return result, nil
}

func validHTTPHeaderName(value string) bool {
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			continue
		}
		if !strings.ContainsRune("!#$%&'*+-.^_`|~", character) {
			return false
		}
	}
	return true
}

func validHTTPHeaderValue(value string) bool {
	for _, character := range value {
		if character == '\r' || character == '\n' || character == 0 || character == 0x7f || (character < 0x20 && character != '\t') {
			return false
		}
	}
	return true
}

func (service *ModelConfigService) writeDocument(root map[string]any) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("encode models.json: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxModelsConfigBytes {
		return errors.New("models.json exceeds the 4 MiB safety limit")
	}
	if err := os.MkdirAll(filepath.Dir(service.modelsPath), 0o700); err != nil {
		return fmt.Errorf("create Pi agent directory: %w", err)
	}
	if err := atomic.WriteFile(service.modelsPath, strings.NewReader(string(data))); err != nil {
		return fmt.Errorf("write models.json: %w", err)
	}
	if err := os.Chmod(service.modelsPath, 0o600); err != nil {
		return fmt.Errorf("restrict models.json permissions: %w", err)
	}
	return nil
}

func (service *ModelConfigService) snapshot(root map[string]any) (domain.ModelConfigSnapshot, error) {
	providers, err := providersObject(root)
	if err != nil {
		return domain.ModelConfigSnapshot{}, err
	}
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := domain.ModelConfigSnapshot{Path: service.modelsPath, Providers: make([]domain.ManagedModelProvider, 0, len(ids))}
	for _, id := range ids {
		provider, _, err := objectValue(providers, id)
		if err != nil {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", id, err)
		}
		models, err := modelObjects(provider)
		if err != nil {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", id, err)
		}
		headers, err := providerHeaders(provider)
		if err != nil {
			return domain.ModelConfigSnapshot{}, fmt.Errorf("provider %s: %w", id, err)
		}
		managed := domain.ManagedModelProvider{
			ID:         id,
			BaseURL:    stringValue(provider["baseUrl"]),
			API:        stringValue(provider["api"]),
			APIKey:     stringValue(provider["apiKey"]),
			Headers:    headers,
			CompatJSON: objectJSON(provider["compat"]),
			Models:     make([]domain.ManagedModel, 0, len(models)),
		}
		for _, model := range models {
			modelID := stringValue(model["id"])
			if modelID == "" {
				continue
			}
			managed.Models = append(managed.Models, domain.ManagedModel{
				ID:                   modelID,
				Name:                 stringValue(model["name"]),
				ContextWindow:        integerValue(model["contextWindow"], defaultContextWindow),
				MaxTokens:            integerValue(model["maxTokens"], defaultMaxTokens),
				Reasoning:            boolValue(model["reasoning"]),
				ImageInput:           includesString(model["input"], "image"),
				ThinkingLevelMapJSON: objectJSON(model["thinkingLevelMap"]),
				CompatJSON:           objectJSON(model["compat"]),
			})
		}
		sort.Slice(managed.Models, func(left, right int) bool { return managed.Models[left].ID < managed.Models[right].ID })
		result.Providers = append(result.Providers, managed)
	}
	return result, nil
}

func providerHeaders(provider map[string]any) (map[string]string, error) {
	value, exists := provider["headers"]
	if !exists || value == nil {
		return nil, nil
	}
	result := map[string]string{}
	switch headers := value.(type) {
	case map[string]string:
		for name, headerValue := range headers {
			result[name] = headerValue
		}
	case map[string]any:
		for name, headerValue := range headers {
			text, ok := headerValue.(string)
			if !ok {
				return nil, fmt.Errorf("provider headers value for %q must be a string", name)
			}
			result[name] = text
		}
	default:
		return nil, errors.New("provider headers must be an object")
	}
	return normalizeProviderHeaders(result)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func integerValue(value any, fallback int) int {
	switch number := value.(type) {
	case json.Number:
		parsed, err := strconv.Atoi(number.String())
		if err == nil && parsed > 0 {
			return parsed
		}
	case float64:
		if number > 0 {
			return int(number)
		}
	case int:
		if number > 0 {
			return number
		}
	}
	return fallback
}

func includesString(value any, target string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if text, ok := item.(string); ok && text == target {
			return true
		}
	}
	return false
}

func objectJSON(value any) string {
	object, ok := value.(map[string]any)
	if !ok || len(object) == 0 {
		return ""
	}
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}
