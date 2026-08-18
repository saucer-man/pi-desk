package appservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"pi-desk/internal/domain"
)

const (
	modelProbeTimeout      = 20 * time.Second
	maxProbeBodyBytes      = 4 << 20
	maxModelTestPromptSize = 16 << 10
	maxDiscoveredModels    = 5000
)

var (
	versionPathPattern          = regexp.MustCompile(`/v\d+(?:alpha|beta)?$`)
	credentialAssignmentPattern = regexp.MustCompile(`(?i)\b(api[-_ ]?key|authorization|x-api-key|x-goog-api-key)\b\s*[:=]\s*(?:bearer\s+)?[^\s,;]+`)
	commonSecretPattern         = regexp.MustCompile(`\b(?:sk|xai|ds)-[A-Za-z0-9_-]{8,}\b`)
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func defaultModelHTTPClient() httpDoer {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (service *ModelConfigService) DiscoverModels(request domain.DiscoverModelsRequest) (domain.ModelDiscoveryResult, error) {
	request.BaseURL = strings.TrimSpace(request.BaseURL)
	request.API = strings.TrimSpace(request.API)
	request.APIKey = strings.TrimSpace(request.APIKey)
	headers, err := normalizeProviderHeaders(request.Headers)
	if err != nil {
		return domain.ModelDiscoveryResult{}, err
	}
	apiKey, err := validateProbeProvider(request.BaseURL, request.API, request.APIKey)
	if err != nil {
		return domain.ModelDiscoveryResult{}, err
	}
	endpoints, err := modelListEndpoints(request.BaseURL, request.API)
	if err != nil {
		return domain.ModelDiscoveryResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), modelProbeTimeout)
	defer cancel()
	var lastError error
	for _, endpoint := range endpoints {
		models, requestErr := service.fetchModels(ctx, endpoint, request.API, apiKey, headers)
		if requestErr == nil {
			return domain.ModelDiscoveryResult{Models: models, Endpoint: endpoint.String()}, nil
		}
		lastError = requestErr
		if ctx.Err() != nil {
			break
		}
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return domain.ModelDiscoveryResult{}, errors.New("model discovery timed out after 20 seconds")
	}
	if lastError == nil {
		lastError = errors.New("no model-list endpoint was available")
	}
	return domain.ModelDiscoveryResult{}, lastError
}

func (service *ModelConfigService) fetchModels(ctx context.Context, endpoint *url.URL, api, apiKey string, headers map[string]string) ([]domain.DiscoveredModel, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create model-list request: %w", err)
	}
	applyProviderHeaders(request.Header, api, apiKey)
	applyCustomProviderHeaders(request.Header, headers)
	response, err := service.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch models: %s", redactProbeText(err.Error(), apiKey))
	}
	defer response.Body.Close()
	body, err := readProbeBody(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, redactProbeText(string(body), apiKey))
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, errors.New("upstream model list was not valid JSON")
	}
	models := parseDiscoveredModels(payload)
	if len(models) == 0 {
		return nil, errors.New("upstream returned an empty model list")
	}
	return models, nil
}

func (service *ModelConfigService) TestModel(request domain.TestModelConfigRequest) (domain.ModelTestResult, error) {
	request.BaseURL = strings.TrimSpace(request.BaseURL)
	request.API = strings.TrimSpace(request.API)
	request.APIKey = strings.TrimSpace(request.APIKey)
	request.ModelID = strings.TrimSpace(request.ModelID)
	request.Prompt = strings.TrimSpace(request.Prompt)
	headers, err := normalizeProviderHeaders(request.Headers)
	if err != nil {
		return domain.ModelTestResult{}, err
	}
	apiKey, err := validateProbeProvider(request.BaseURL, request.API, request.APIKey)
	if err != nil {
		return domain.ModelTestResult{}, err
	}
	if err := validateIdentifier("model id", request.ModelID); err != nil {
		return domain.ModelTestResult{}, err
	}
	if request.Prompt == "" {
		return domain.ModelTestResult{}, errors.New("model test prompt is required")
	}
	if len(request.Prompt) > maxModelTestPromptSize {
		return domain.ModelTestResult{}, fmt.Errorf("model test prompt exceeds %d bytes", maxModelTestPromptSize)
	}
	endpoint, payload, err := buildModelTestRequest(request.BaseURL, request.API, request.ModelID, request.Prompt)
	if err != nil {
		return domain.ModelTestResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), modelProbeTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return domain.ModelTestResult{}, fmt.Errorf("create model test request: %w", err)
	}
	applyProviderHeaders(httpRequest.Header, request.API, apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	applyCustomProviderHeaders(httpRequest.Header, headers)
	started := time.Now()
	response, requestErr := service.client.Do(httpRequest)
	result := domain.ModelTestResult{}
	if requestErr != nil {
		result.LatencyMS = time.Since(started).Milliseconds()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Error = "Model test timed out after 20 seconds"
		} else {
			result.Error = redactProbeText(requestErr.Error(), apiKey)
		}
		return result, nil
	}
	defer response.Body.Close()
	result.Status = response.StatusCode
	body, readErr := readProbeBody(response.Body)
	result.LatencyMS = time.Since(started).Milliseconds()
	if readErr != nil {
		result.Error = readErr.Error()
		return result, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		result.Error = fmt.Sprintf("HTTP %d: %s", response.StatusCode, redactProbeText(string(body), apiKey))
		return result, nil
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		result.Error = "Provider returned a successful response that was not valid JSON"
		return result, nil
	}
	result.OK = true
	result.Response = extractProbeText(decoded, request.API)
	if result.Response == "" {
		result.Response = "OK"
	}
	return result, nil
}

func (service *ModelConfigService) GetAccountQuota(request domain.ModelQuotaRequest) (domain.ModelQuotaResult, error) {
	request.BaseURL = strings.TrimSpace(request.BaseURL)
	request.API = strings.TrimSpace(request.API)
	request.APIKey = strings.TrimSpace(request.APIKey)
	headers, err := normalizeProviderHeaders(request.Headers)
	if err != nil {
		return domain.ModelQuotaResult{}, err
	}
	apiKey, err := validateProbeProvider(request.BaseURL, request.API, request.APIKey)
	if err != nil {
		return domain.ModelQuotaResult{}, err
	}
	endpoint, err := modelQuotaEndpoint(request.BaseURL, request.API)
	if err != nil {
		return domain.ModelQuotaResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), modelProbeTimeout)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return domain.ModelQuotaResult{}, fmt.Errorf("create account quota request: %w", err)
	}
	applyProviderHeaders(httpRequest.Header, request.API, apiKey)
	applyCustomProviderHeaders(httpRequest.Header, headers)
	started := time.Now()
	response, requestErr := service.client.Do(httpRequest)
	result := domain.ModelQuotaResult{Endpoint: endpoint.String()}
	if requestErr != nil {
		result.LatencyMS = time.Since(started).Milliseconds()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Error = "Account quota request timed out after 20 seconds"
		} else {
			result.Error = redactProbeText(requestErr.Error(), apiKey)
		}
		return result, nil
	}
	defer response.Body.Close()
	result.Status = response.StatusCode
	body, readErr := readProbeBody(response.Body)
	result.LatencyMS = time.Since(started).Milliseconds()
	if readErr != nil {
		result.Error = readErr.Error()
		return result, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
			result.Error = fmt.Sprintf("The provider does not expose account quota at %s", endpoint.String())
		} else {
			result.Error = fmt.Sprintf("HTTP %d: %s", response.StatusCode, redactProbeText(string(body), apiKey))
		}
		return result, nil
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		result.Error = "Quota endpoint returned a successful response that was not valid JSON"
		return result, nil
	}
	formatted, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		result.Error = "Unable to format the quota response"
		return result, nil
	}
	result.OK = true
	result.Summary = redactProbeText(quotaSummary(decoded), apiKey)
	result.DetailsJSON = redactProbeText(string(formatted), apiKey)
	return result, nil
}

func modelQuotaEndpoint(rawBaseURL, api string) (*url.URL, error) {
	base, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, errors.New("base URL is invalid")
	}
	endpoint := cloneURL(base)
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	host := strings.ToLower(endpoint.Hostname())
	switch {
	case host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai"):
		endpoint.Path = "/api/v1/auth/key"
	case host == "api.deepseek.com" || strings.HasSuffix(host, ".deepseek.com"):
		endpoint.Path = "/user/balance"
	case host == "api.moonshot.cn" || strings.HasSuffix(host, ".moonshot.cn"):
		endpoint.Path = "/v1/users/me/balance"
	case host == "api.siliconflow.cn" || strings.HasSuffix(host, ".siliconflow.cn"):
		endpoint.Path = "/v1/user/info"
	case strings.HasPrefix(api, "openai-"):
		prefix := strings.TrimRight(endpoint.Path, "/")
		if versionPathPattern.MatchString(strings.ToLower(prefix)) {
			prefix = prefix[:strings.LastIndex(prefix, "/")]
		}
		endpoint.Path = joinURLPath(prefix, "dashboard", "billing", "credit_grants")
	default:
		return nil, errors.New("this provider API does not define an account quota endpoint")
	}
	return endpoint, nil
}

var quotaSummaryKeys = map[string]struct{}{
	"available_balance": {}, "balance": {}, "cash_balance": {}, "currency": {}, "granted_balance": {},
	"is_available": {}, "is_free_tier": {}, "limit": {}, "limit_remaining": {}, "total_available": {},
	"total_balance": {}, "total_granted": {}, "total_used": {}, "topped_up_balance": {}, "usage": {},
	"voucher_balance": {},
}

func quotaSummary(value any) string {
	lines := make([]string, 0, 12)
	var collect func(any, string)
	collect = func(current any, prefix string) {
		if len(lines) >= 12 {
			return
		}
		switch typed := current.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				item := typed[key]
				path := key
				if prefix != "" {
					path = prefix + "." + key
				}
				if _, wanted := quotaSummaryKeys[strings.ToLower(key)]; wanted {
					if formatted, ok := quotaScalar(item); ok {
						lines = append(lines, fmt.Sprintf("%s: %s", path, formatted))
					}
				}
				collect(item, path)
			}
		case []any:
			for index, item := range typed {
				collect(item, fmt.Sprintf("%s[%d]", prefix, index))
			}
		}
	}
	collect(value, "")
	if len(lines) == 0 {
		return "Quota endpoint returned JSON; inspect the full response below."
	}
	return strings.Join(lines, "\n")
}

func quotaScalar(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(typed), true
	default:
		return "", false
	}
}

func validateProbeProvider(baseURL, api, configuredKey string) (string, error) {
	if baseURL == "" {
		return "", errors.New("base URL is required for direct provider requests")
	}
	if err := validateBaseURL(baseURL); err != nil {
		return "", err
	}
	if _, ok := supportedModelAPIs[api]; !ok {
		return "", errors.New("select a supported API type before testing or fetching models")
	}
	return resolveProbeAPIKey(configuredKey)
}

func resolveProbeAPIKey(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if strings.HasPrefix(value, "!") {
		return "", errors.New("direct provider requests do not execute !command credentials; use a literal key or environment variable")
	}
	if strings.HasPrefix(value, "${") {
		if !strings.HasSuffix(value, "}") || len(value) < 4 {
			return "", errors.New("credential environment reference is invalid")
		}
		return environmentCredential(value[2 : len(value)-1])
	}
	if strings.HasPrefix(value, "$") {
		return environmentCredential(value[1:])
	}
	return value, nil
}

func environmentCredential(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("credential environment reference is invalid")
	}
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("environment variable %s is not set", name)
	}
	return strings.TrimSpace(value), nil
}

func modelListEndpoints(rawBaseURL, api string) ([]*url.URL, error) {
	base, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, errors.New("base URL is invalid")
	}
	trimmedPath := strings.TrimRight(base.Path, "/")
	if strings.HasSuffix(strings.ToLower(trimmedPath), "/models") {
		endpoint := cloneURL(base)
		addModelListQuery(endpoint, api)
		return []*url.URL{endpoint}, nil
	}
	version := "v1"
	if api == "google-generative-ai" {
		version = "v1beta"
	}
	primaryBase := cloneURL(base)
	if !versionPathPattern.MatchString(strings.ToLower(trimmedPath)) {
		primaryBase.Path = joinURLPath(trimmedPath, version)
	}
	primaryBase.Path = joinURLPath(primaryBase.Path, "models")
	addModelListQuery(primaryBase, api)
	if api == "google-generative-ai" || versionPathPattern.MatchString(strings.ToLower(trimmedPath)) {
		return []*url.URL{primaryBase}, nil
	}
	fallback := cloneURL(base)
	fallback.Path = joinURLPath(trimmedPath, "models")
	addModelListQuery(fallback, api)
	if fallback.String() == primaryBase.String() {
		return []*url.URL{primaryBase}, nil
	}
	return []*url.URL{primaryBase, fallback}, nil
}

func addModelListQuery(endpoint *url.URL, api string) {
	query := endpoint.Query()
	if api == "anthropic-messages" && !query.Has("limit") {
		query.Set("limit", "1000")
	}
	if api == "google-generative-ai" && !query.Has("pageSize") {
		query.Set("pageSize", "1000")
	}
	endpoint.RawQuery = query.Encode()
}

func buildModelTestRequest(rawBaseURL, api, modelID, prompt string) (*url.URL, []byte, error) {
	base, err := url.Parse(rawBaseURL)
	if err != nil {
		return nil, nil, errors.New("base URL is invalid")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	version := "v1"
	if api == "google-generative-ai" {
		version = "v1beta"
	}
	if !versionPathPattern.MatchString(strings.ToLower(base.Path)) {
		base.Path = joinURLPath(base.Path, version)
	}
	var body any
	switch api {
	case "openai-responses":
		base.Path = joinURLPath(base.Path, "responses")
		body = map[string]any{"model": modelID, "input": prompt, "max_output_tokens": 128}
	case "anthropic-messages":
		base.Path = joinURLPath(base.Path, "messages")
		body = map[string]any{"model": modelID, "messages": []any{map[string]any{"role": "user", "content": prompt}}, "max_tokens": 128}
	case "google-generative-ai":
		cleanModelID := strings.TrimPrefix(modelID, "models/")
		base.Path = joinURLPath(base.Path, "models", cleanModelID+":generateContent")
		body = map[string]any{
			"contents":         []any{map[string]any{"role": "user", "parts": []any{map[string]any{"text": prompt}}}},
			"generationConfig": map[string]any{"maxOutputTokens": 128},
		}
	default:
		base.Path = joinURLPath(base.Path, "chat", "completions")
		body = map[string]any{"model": modelID, "messages": []any{map[string]any{"role": "user", "content": prompt}}, "max_tokens": 128, "stream": false}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("encode model test request: %w", err)
	}
	return base, payload, nil
}

func applyProviderHeaders(headers http.Header, api, apiKey string) {
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", "Pi-Desk/0.1")
	if apiKey == "" {
		return
	}
	switch api {
	case "anthropic-messages":
		headers.Set("x-api-key", apiKey)
		headers.Set("anthropic-version", "2023-06-01")
	case "google-generative-ai":
		headers.Set("x-goog-api-key", apiKey)
	default:
		headers.Set("Authorization", "Bearer "+apiKey)
	}
}

func applyCustomProviderHeaders(headers http.Header, configured map[string]string) {
	for name, value := range configured {
		headers.Del(name)
		headers.Set(name, value)
	}
}

func parseDiscoveredModels(value any) []domain.DiscoveredModel {
	items := modelListItems(value)
	seen := make(map[string]struct{}, len(items))
	models := make([]domain.DiscoveredModel, 0, len(items))
	for _, item := range items {
		model, ok := discoveredModelFromValue(item)
		if !ok {
			continue
		}
		if _, exists := seen[model.ID]; exists {
			continue
		}
		seen[model.ID] = struct{}{}
		models = append(models, model)
		if len(models) == maxDiscoveredModels {
			break
		}
	}
	sort.Slice(models, func(left, right int) bool {
		leftName := models[left].Name
		if leftName == "" {
			leftName = models[left].ID
		}
		rightName := models[right].Name
		if rightName == "" {
			rightName = models[right].ID
		}
		return strings.ToLower(leftName) < strings.ToLower(rightName)
	})
	return models
}

func modelListItems(value any) []any {
	if items, ok := value.([]any); ok {
		return items
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for _, key := range []string{"data", "models", "results", "items"} {
		if items, ok := object[key].([]any); ok {
			return items
		}
		if values, ok := object[key].(map[string]any); ok {
			items := make([]any, 0, len(values))
			for id, raw := range values {
				if model, ok := raw.(map[string]any); ok {
					if _, exists := model["id"]; !exists {
						model["id"] = id
					}
				}
				items = append(items, raw)
			}
			return items
		}
	}
	return nil
}

func discoveredModelFromValue(value any) (domain.DiscoveredModel, bool) {
	if text, ok := value.(string); ok {
		id := strings.TrimPrefix(strings.TrimSpace(text), "models/")
		return domain.DiscoveredModel{ID: id}, id != ""
	}
	object, ok := value.(map[string]any)
	if !ok {
		return domain.DiscoveredModel{}, false
	}
	rawID := firstNonEmptyString(object["id"], object["model"], object["name"])
	id := strings.TrimPrefix(rawID, "models/")
	if id == "" {
		return domain.DiscoveredModel{}, false
	}
	name := firstNonEmptyString(object["display_name"], object["displayName"])
	if name == "" && (stringValue(object["id"]) != "" || stringValue(object["model"]) != "") {
		name = strings.TrimPrefix(stringValue(object["name"]), "models/")
	}
	if name == id {
		name = ""
	}
	return domain.DiscoveredModel{
		ID:            id,
		Name:          name,
		ContextWindow: firstPositiveInt(object, "contextWindow", "context_window", "context_length", "max_context_window"),
		MaxTokens:     firstPositiveInt(object, "maxTokens", "max_tokens", "max_output_tokens", "max_output_token_limit"),
		Reasoning:     firstBool(object, "reasoning", "supports_reasoning", "supportsReasoning", "thinking"),
		ImageInput:    supportsImageInput(object),
	}, true
}

func firstPositiveInt(object map[string]any, keys ...string) int {
	for _, key := range keys {
		value, exists := object[key]
		if !exists {
			continue
		}
		var number int
		switch value := value.(type) {
		case float64:
			number = int(value)
		case json.Number:
			number, _ = strconv.Atoi(value.String())
		case int:
			number = value
		}
		if number > 0 {
			return number
		}
	}
	return 0
}

func firstBool(object map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := object[key].(bool); ok {
			return value
		}
	}
	return false
}

func supportsImageInput(object map[string]any) bool {
	for _, key := range []string{"imageInput", "supports_vision", "supportsVision", "vision"} {
		if value, ok := object[key].(bool); ok && value {
			return true
		}
	}
	for _, key := range []string{"input", "input_modalities", "modalities"} {
		if values, ok := object[key].([]any); ok {
			for _, value := range values {
				if text, ok := value.(string); ok && (strings.EqualFold(text, "image") || strings.EqualFold(text, "vision")) {
					return true
				}
			}
		}
	}
	return false
}

func extractProbeText(value any, api string) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	var text string
	switch api {
	case "openai-responses":
		text = stringValue(object["output_text"])
		if text == "" {
			text = nestedText(object["output"], "content", "text")
		}
	case "anthropic-messages":
		text = nestedText(object["content"], "", "text")
	case "google-generative-ai":
		text = nestedText(object["candidates"], "content", "parts", "text")
	default:
		text = nestedText(object["choices"], "message", "content")
		if text == "" {
			text = nestedText(object["choices"], "", "text")
		}
	}
	return boundedText(text, 300)
}

func nestedText(value any, path ...string) string {
	current := value
	for _, key := range path {
		if items, ok := current.([]any); ok {
			if len(items) == 0 {
				return ""
			}
			current = items[0]
		}
		if key == "" {
			continue
		}
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	if items, ok := current.([]any); ok {
		var result strings.Builder
		for _, item := range items {
			if text := nestedText(item); text != "" {
				result.WriteString(text)
			}
		}
		return result.String()
	}
	if object, ok := current.(map[string]any); ok {
		return firstNonEmptyString(object["text"], object["content"])
	}
	return stringValue(current)
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		if text := stringValue(value); text != "" {
			return text
		}
	}
	return ""
}

func readProbeBody(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxProbeBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read provider response: %w", err)
	}
	if len(data) > maxProbeBodyBytes {
		return nil, errors.New("provider response exceeds the 4 MiB safety limit")
	}
	return data, nil
}

func redactProbeText(value, apiKey string) string {
	text := strings.TrimSpace(value)
	if apiKey != "" {
		text = strings.ReplaceAll(text, apiKey, "[redacted]")
	}
	text = credentialAssignmentPattern.ReplaceAllString(text, "$1=[redacted]")
	text = commonSecretPattern.ReplaceAllString(text, "[redacted]")
	return boundedText(text, 2000)
}

func boundedText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n... output truncated"
}

func cloneURL(value *url.URL) *url.URL {
	clone := *value
	return &clone
}

func joinURLPath(base string, parts ...string) string {
	path := strings.TrimRight(base, "/")
	for _, part := range parts {
		path += "/" + strings.Trim(part, "/")
	}
	if path == "" {
		return "/"
	}
	return strings.ReplaceAll(path, "//", "/")
}
