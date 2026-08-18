package appservice

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"pi-desk/internal/domain"
)

func TestDiscoverModelsFetchesAndParsesOpenAIList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer visible-key" {
			t.Fatalf("missing bearer credential: %#v", request.Header)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"data": []any{
			map[string]any{"id": "gpt-b", "name": "GPT B", "context_window": 272000, "max_output_tokens": 128000, "reasoning": true, "input": []any{"text", "image"}},
			map[string]any{"id": "gpt-a"},
			map[string]any{"id": "gpt-a"},
		}})
	}))
	defer server.Close()
	service := newModelConfigService("", server.Client())

	result, err := service.DiscoverModels(domain.DiscoverModelsRequest{
		BaseURL: server.URL, API: "openai-responses", APIKey: "visible-key",
	})
	if err != nil {
		t.Fatalf("DiscoverModels returned an error: %v", err)
	}
	if len(result.Models) != 2 || result.Models[0].Name != "GPT B" || result.Models[0].ContextWindow != 272000 || result.Models[0].MaxTokens != 128000 || !result.Models[0].Reasoning || !result.Models[0].ImageInput || result.Models[1].ID != "gpt-a" {
		t.Fatalf("unexpected discovery result: %#v", result)
	}
	if !strings.HasSuffix(result.Endpoint, "/v1/models") {
		t.Fatalf("unexpected endpoint: %s", result.Endpoint)
	}
}

func TestDiscoverModelsFallsBackToUnversionedEndpoint(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.URL.Path == "/v1/models" {
			http.Error(response, "not found", http.StatusNotFound)
			return
		}
		if request.URL.Path != "/models" {
			t.Fatalf("unexpected fallback path: %s", request.URL.Path)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"models": []any{
			map[string]any{"name": "models/gem-compatible", "displayName": "Gem Compatible"},
		}})
	}))
	defer server.Close()
	service := newModelConfigService("", server.Client())

	result, err := service.DiscoverModels(domain.DiscoverModelsRequest{BaseURL: server.URL, API: "openai-completions"})
	if err != nil {
		t.Fatalf("DiscoverModels returned an error: %v", err)
	}
	if requests.Load() != 2 || len(result.Models) != 1 || result.Models[0].ID != "gem-compatible" {
		t.Fatalf("unexpected fallback result: requests=%d result=%#v", requests.Load(), result)
	}
}

func TestModelListEndpointsUseProtocolSpecificPaths(t *testing.T) {
	tests := []struct {
		api      string
		expected string
	}{
		{api: "openai-completions", expected: "https://example.test/v1/models"},
		{api: "openai-responses", expected: "https://example.test/v1/models"},
		{api: "anthropic-messages", expected: "https://example.test/v1/models?limit=1000"},
		{api: "google-generative-ai", expected: "https://example.test/v1beta/models?pageSize=1000"},
	}
	for _, test := range tests {
		t.Run(test.api, func(t *testing.T) {
			endpoints, err := modelListEndpoints("https://example.test", test.api)
			if err != nil {
				t.Fatalf("modelListEndpoints returned an error: %v", err)
			}
			if endpoints[0].String() != test.expected {
				t.Fatalf("expected %s, got %s", test.expected, endpoints[0])
			}
		})
	}
}

func TestDirectModelTestSupportsOfficialPiAPIs(t *testing.T) {
	tests := []struct {
		api          string
		path         string
		authHeader   string
		authValue    string
		responseBody any
	}{
		{api: "openai-completions", path: "/v1/chat/completions", authHeader: "Authorization", authValue: "Bearer direct-key", responseBody: map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "chat ok"}}}}},
		{api: "openai-responses", path: "/v1/responses", authHeader: "Authorization", authValue: "Bearer direct-key", responseBody: map[string]any{"output_text": "responses ok"}},
		{api: "anthropic-messages", path: "/v1/messages", authHeader: "x-api-key", authValue: "direct-key", responseBody: map[string]any{"content": []any{map[string]any{"type": "text", "text": "anthropic ok"}}}},
		{api: "google-generative-ai", path: "/v1beta/models/gemini-test:generateContent", authHeader: "x-goog-api-key", authValue: "direct-key", responseBody: map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"parts": []any{map[string]any{"text": "google ok"}}}}}}},
	}
	for _, test := range tests {
		t.Run(test.api, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != test.path {
					t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
				}
				if request.Header.Get(test.authHeader) != test.authValue {
					t.Fatalf("unexpected auth header: %#v", request.Header)
				}
				var payload any
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Fatalf("decode request body: %v", err)
				}
				encoded, _ := json.Marshal(payload)
				if !strings.Contains(string(encoded), "Confirm this model works") {
					t.Fatalf("custom prompt missing from request: %s", encoded)
				}
				_ = json.NewEncoder(response).Encode(test.responseBody)
			}))
			defer server.Close()
			service := newModelConfigService("", server.Client())

			result, err := service.TestModel(domain.TestModelConfigRequest{
				BaseURL: server.URL, API: test.api, APIKey: "direct-key", ModelID: "gemini-test", Prompt: "Confirm this model works",
			})
			if err != nil {
				t.Fatalf("TestModel returned an error: %v", err)
			}
			if !result.OK || result.Status != http.StatusOK || !strings.Contains(result.Response, "ok") {
				t.Fatalf("unexpected test result: %#v", result)
			}
		})
	}
}

func TestDirectModelTestResolvesEnvironmentCredential(t *testing.T) {
	t.Setenv("PI_DESK_TEST_KEY", "resolved-key")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer resolved-key" {
			t.Fatalf("environment credential was not resolved: %#v", request.Header)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"choices": []any{
			map[string]any{"message": map[string]any{"content": "OK"}},
		}})
	}))
	defer server.Close()
	service := newModelConfigService("", server.Client())

	result, err := service.TestModel(domain.TestModelConfigRequest{
		BaseURL: server.URL, API: "openai-completions", APIKey: "$PI_DESK_TEST_KEY", ModelID: "test", Prompt: "Confirm availability",
	})
	if err != nil || !result.OK {
		t.Fatalf("unexpected result: result=%#v err=%v", result, err)
	}
}

func TestAccountQuotaUsesOpenAICompatibleCreditEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/dashboard/billing/credit_grants" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer quota-key" {
			t.Fatalf("missing quota credential: %#v", request.Header)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"total_granted": 20.0, "total_used": 7.25, "total_available": 12.75,
		})
	}))
	defer server.Close()
	service := newModelConfigService("", server.Client())

	result, err := service.GetAccountQuota(domain.ModelQuotaRequest{
		BaseURL: server.URL + "/v1", API: "openai-responses", APIKey: "quota-key",
	})
	if err != nil {
		t.Fatalf("GetAccountQuota returned an error: %v", err)
	}
	if !result.OK || result.Status != http.StatusOK || !strings.Contains(result.Summary, "total_available: 12.75") || !strings.Contains(result.DetailsJSON, "total_used") {
		t.Fatalf("unexpected quota result: %#v", result)
	}
}

func TestAccountQuotaReportsUnsupportedProviderAPI(t *testing.T) {
	service := newModelConfigService("", nil)
	_, err := service.GetAccountQuota(domain.ModelQuotaRequest{
		BaseURL: "https://api.anthropic.com", API: "anthropic-messages", APIKey: "quota-key",
	})
	if err == nil || !strings.Contains(err.Error(), "does not define an account quota endpoint") {
		t.Fatalf("expected unsupported quota endpoint error, got %v", err)
	}
}

func TestAccountQuotaRecognizesDocumentedProviderEndpoints(t *testing.T) {
	tests := []struct {
		baseURL string
		api     string
		want    string
	}{
		{"https://openrouter.ai/api/v1", "openai-completions", "https://openrouter.ai/api/v1/auth/key"},
		{"https://api.deepseek.com/v1", "openai-completions", "https://api.deepseek.com/user/balance"},
		{"https://api.moonshot.cn/v1", "openai-completions", "https://api.moonshot.cn/v1/users/me/balance"},
		{"https://api.siliconflow.cn/v1", "openai-completions", "https://api.siliconflow.cn/v1/user/info"},
	}
	for _, test := range tests {
		endpoint, err := modelQuotaEndpoint(test.baseURL, test.api)
		if err != nil || endpoint.String() != test.want {
			t.Fatalf("modelQuotaEndpoint(%q) = %v, %v; want %q", test.baseURL, endpoint, err, test.want)
		}
	}
}

func TestCustomProviderHeadersReachAllDirectRequestsAndOverrideDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("User-Agent") != "custom-client" || request.Header.Get("Authorization") != "Custom credential" || request.Header.Get("X-Channel") != "desktop" {
			t.Fatalf("custom headers were not applied last: %#v", request.Header)
		}
		switch request.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(response).Encode(map[string]any{"data": []any{map[string]any{"id": "test-model"}}})
		case "/v1/responses":
			_ = json.NewEncoder(response).Encode(map[string]any{"output_text": "model ok"})
		case "/dashboard/billing/credit_grants":
			_ = json.NewEncoder(response).Encode(map[string]any{"total_available": 5})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	service := newModelConfigService("", server.Client())
	headers := map[string]string{
		"User-Agent":    "custom-client",
		"authorization": "Custom credential",
		"X-Channel":     "desktop",
	}

	if _, err := service.DiscoverModels(domain.DiscoverModelsRequest{
		BaseURL: server.URL + "/v1", API: "openai-responses", APIKey: "automatic-key", Headers: headers,
	}); err != nil {
		t.Fatalf("DiscoverModels returned an error: %v", err)
	}
	testResult, err := service.TestModel(domain.TestModelConfigRequest{
		BaseURL: server.URL + "/v1", API: "openai-responses", APIKey: "automatic-key", Headers: headers,
		ModelID: "test-model", Prompt: "Confirm availability",
	})
	if err != nil || !testResult.OK {
		t.Fatalf("unexpected model test: result=%#v err=%v", testResult, err)
	}
	quotaResult, err := service.GetAccountQuota(domain.ModelQuotaRequest{
		BaseURL: server.URL + "/v1", API: "openai-responses", APIKey: "automatic-key", Headers: headers,
	})
	if err != nil || !quotaResult.OK {
		t.Fatalf("unexpected quota request: result=%#v err=%v", quotaResult, err)
	}
}

func TestDirectModelTestRequiresPrompt(t *testing.T) {
	service := newModelConfigService("", nil)
	_, err := service.TestModel(domain.TestModelConfigRequest{
		BaseURL: "https://example.test/v1", API: "openai-completions", ModelID: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("expected prompt validation error, got %v", err)
	}
}

func TestDirectModelTestRejectsCommandCredential(t *testing.T) {
	service := newModelConfigService("", nil)
	_, err := service.TestModel(domain.TestModelConfigRequest{
		BaseURL: "https://example.test/v1", API: "openai-completions", APIKey: "!secret-command", ModelID: "test", Prompt: "Confirm availability",
	})
	if err == nil || !strings.Contains(err.Error(), "do not execute !command") {
		t.Fatalf("expected command credential error, got %v", err)
	}
}

func TestDirectModelTestRedactsProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(response, "Authorization: Bearer direct-secret-value")
	}))
	defer server.Close()
	service := newModelConfigService("", server.Client())

	result, err := service.TestModel(domain.TestModelConfigRequest{
		BaseURL: server.URL, API: "openai-responses", APIKey: "direct-secret-value", ModelID: "test", Prompt: "Confirm availability",
	})
	if err != nil {
		t.Fatalf("TestModel returned an error: %v", err)
	}
	if result.OK || !strings.Contains(result.Error, "HTTP 401") || strings.Contains(result.Error, "direct-secret-value") {
		t.Fatalf("provider error was not safely returned: %#v", result)
	}
}
