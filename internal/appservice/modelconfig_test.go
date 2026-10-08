package appservice

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pi-desk/internal/domain"
)

func TestGetModelsConfigReturnsEmptyDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent", "models.json")
	service := newModelConfigService(path, nil)

	snapshot, err := service.GetModelsConfig()
	if err != nil {
		t.Fatalf("GetModelsConfig returned an error: %v", err)
	}
	if snapshot.Path != path || len(snapshot.Providers) != 0 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
}

func TestGetConfiguredModelsReturnsNoCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	data := `{"providers":{"custom":{"baseUrl":"https://example.com/v1","apiKey":"secret-value","models":[{"id":"gpt-5","name":"GPT 5","contextWindow":128000,"maxTokens":16384,"reasoning":true}]}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newModelConfigService(path, nil)

	models, err := service.GetConfiguredModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].Provider != "custom" || models[0].ID != "gpt-5" || !models[0].Reasoning {
		t.Fatalf("models = %#v", models)
	}
	encoded, err := json.Marshal(models)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-value") || strings.Contains(string(encoded), "apiKey") {
		t.Fatalf("selectable model response leaked credentials: %s", encoded)
	}
}

func TestUpsertModelCreatesOfficialPiShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent", "models.json")
	service := newModelConfigService(path, nil)
	reserveTokens, keepRecentTokens := 24000, 32000
	imageMaxWidth, imageMaxHeight, imageMaxBytes, imageJPEGQuality := 1568, 1568, 524288, 75

	snapshot, err := service.UpsertModel(domain.UpsertModelConfigRequest{
		ProviderID:           "custom-openai",
		BaseURL:              "https://gateway.example.com/v1",
		API:                  "openai-responses",
		APIKey:               "sk-local-visible-key",
		Headers:              map[string]string{"User-Agent": "custom-agent", "X-Channel": "desktop"},
		ProviderCompatJSON:   `{"supportsDeveloperRole":true}`,
		ModelID:              "gpt-test",
		ModelName:            "GPT Test",
		ContextWindow:        200000,
		MaxTokens:            32000,
		Reasoning:            true,
		ImageInput:           true,
		ReserveTokens:        &reserveTokens,
		KeepRecentTokens:     &keepRecentTokens,
		ImageMaxWidth:        &imageMaxWidth,
		ImageMaxHeight:       &imageMaxHeight,
		ImageMaxBytes:        &imageMaxBytes,
		ImageJPEGQuality:     &imageJPEGQuality,
		ThinkingLevelMapJSON: `{"off":null,"xhigh":"xhigh","max":"max"}`,
		ModelCompatJSON:      `{"supportsReasoningEffort":true}`,
	})
	if err != nil {
		t.Fatalf("UpsertModel returned an error: %v", err)
	}
	if len(snapshot.Providers) != 1 || snapshot.Providers[0].APIKey != "sk-local-visible-key" || snapshot.Providers[0].Headers["User-Agent"] != "custom-agent" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read models.json: %v", err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("decode models.json: %v", err)
	}
	provider := root["providers"].(map[string]any)["custom-openai"].(map[string]any)
	if provider["apiKey"] != "sk-local-visible-key" || provider["api"] != "openai-responses" {
		t.Fatalf("unexpected provider: %#v", provider)
	}
	headers := provider["headers"].(map[string]any)
	if headers["User-Agent"] != "custom-agent" || headers["X-Channel"] != "desktop" {
		t.Fatalf("unexpected provider headers: %#v", headers)
	}
	model := provider["models"].([]any)[0].(map[string]any)
	if model["id"] != "gpt-test" || model["reasoning"] != true || model["contextWindow"] != float64(200000) {
		t.Fatalf("unexpected model: %#v", model)
	}
	resize := model["inputLimits"].(map[string]any)["images"].(map[string]any)["resize"].(map[string]any)
	if resize["maxWidth"] != float64(1568) || resize["maxBytes"] != float64(524288) || resize["jpegQuality"] != float64(75) {
		t.Fatalf("unexpected image resize profile: %#v", resize)
	}
	thinkingLevelMap, ok := model["thinkingLevelMap"].(map[string]any)
	_, hasOff := thinkingLevelMap["off"]
	if !ok || !hasOff || thinkingLevelMap["off"] != nil || thinkingLevelMap["xhigh"] != "xhigh" || thinkingLevelMap["max"] != "max" {
		t.Fatalf("unexpected thinking level map: %#v", model["thinkingLevelMap"])
	}
	if snapshot.Providers[0].Models[0].ThinkingLevelMapJSON == "" {
		t.Fatal("thinking level map was not returned to the editor")
	}
	managed := snapshot.Providers[0].Models[0]
	if managed.ReserveTokens == nil || *managed.ReserveTokens != reserveTokens || managed.ImageJPEGQuality == nil || *managed.ImageJPEGQuality != imageJPEGQuality {
		t.Fatalf("Pi 0.87 limits were not returned to the editor: %#v", managed)
	}
	settingsData, err := os.ReadFile(filepath.Join(filepath.Dir(path), "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	if !strings.Contains(string(settingsData), `"custom-openai/gpt-test"`) || !strings.Contains(string(settingsData), `"reserveTokens": 24000`) {
		t.Fatalf("model compaction override was not written:\n%s", settingsData)
	}
}

func TestUpsertModelDefaultsNewProviderUserAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	service := newModelConfigService(path, nil)

	_, err := service.UpsertModel(domain.UpsertModelConfigRequest{
		ProviderID: "new-provider", API: "openai-completions", ModelID: "model",
		ContextWindow: 128000, MaxTokens: 16384,
	})
	if err != nil {
		t.Fatalf("UpsertModel returned an error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read models.json: %v", err)
	}
	if !strings.Contains(string(data), `"User-Agent": "codex_cli_rs/0.146.0 (Windows 11.0.26100; x86_64) Terminal"`) {
		t.Fatalf("new provider did not receive the default user agent:\n%s", data)
	}
}

func TestUpsertModelDoesNotDefaultExistingProviderHeaders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	fixture := `{"providers":{"existing":{"models":[{"id":"model"}]}}}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newModelConfigService(path, nil)

	_, err := service.UpsertModel(domain.UpsertModelConfigRequest{
		OriginalProviderID: "existing", OriginalModelID: "model", ProviderID: "existing", ModelID: "model",
		ContextWindow: 128000, MaxTokens: 16384,
	})
	if err != nil {
		t.Fatalf("UpsertModel returned an error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "User-Agent") {
		t.Fatalf("existing provider received an implicit user agent:\n%s", data)
	}
}

func TestUpsertModelPreservesUnknownFieldsAndReturnsCredential(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "models.json")
	existing := `{
  "futureRoot": {"enabled": true},
  "providers": {
    "deepseek-proxy": {
      "apiKey": "literal-secret-that-must-not-be-returned",
      "headers": {"X-Secret": "hidden"},
      "futureProvider": 42,
      "models": [{
        "id": "deepseek-reasoner",
        "name": "Old name",
        "futureModel": {"mode": "keep"},
        "contextWindow": 64000,
        "maxTokens": 8000
      }]
    }
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	service := newModelConfigService(path, nil)

	snapshot, err := service.UpsertModel(domain.UpsertModelConfigRequest{
		OriginalProviderID: "deepseek-proxy",
		OriginalModelID:    "deepseek-reasoner",
		ProviderID:         "deepseek-proxy",
		API:                "openai-completions",
		APIKey:             "literal-secret-that-must-not-be-returned",
		ModelID:            "deepseek-reasoner",
		ModelName:          "DeepSeek Reasoner",
		ContextWindow:      128000,
		MaxTokens:          16000,
		Reasoning:          true,
	})
	if err != nil {
		t.Fatalf("UpsertModel returned an error: %v", err)
	}
	if snapshot.Providers[0].APIKey != "literal-secret-that-must-not-be-returned" || snapshot.Providers[0].Headers["X-Secret"] != "hidden" {
		t.Fatalf("provider credentials or headers were not returned to the model editor: %#v", snapshot)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read models.json: %v", err)
	}
	if !strings.Contains(string(data), "futureRoot") || !strings.Contains(string(data), "futureProvider") || !strings.Contains(string(data), "futureModel") {
		t.Fatalf("unknown fields were not preserved:\n%s", data)
	}
	if !strings.Contains(string(data), "literal-secret-that-must-not-be-returned") || !strings.Contains(string(data), "X-Secret") {
		t.Fatalf("existing secret configuration was unexpectedly removed:\n%s", data)
	}
}

func TestUpsertModelRejectsUnsafeOrInvalidFields(t *testing.T) {
	service := newModelConfigService(filepath.Join(t.TempDir(), "models.json"), nil)
	base := domain.UpsertModelConfigRequest{
		ProviderID: "provider", API: "openai-completions", ModelID: "model", ContextWindow: 128000, MaxTokens: 16000,
	}

	tests := []struct {
		name   string
		mutate func(*domain.UpsertModelConfigRequest)
	}{
		{name: "multiline key", mutate: func(request *domain.UpsertModelConfigRequest) { request.APIKey = "key\nvalue" }},
		{name: "invalid header name", mutate: func(request *domain.UpsertModelConfigRequest) {
			request.Headers = map[string]string{"Bad Header": "value"}
		}},
		{name: "multiline header value", mutate: func(request *domain.UpsertModelConfigRequest) {
			request.Headers = map[string]string{"X-Test": "first\nsecond"}
		}},
		{name: "duplicate header name", mutate: func(request *domain.UpsertModelConfigRequest) {
			request.Headers = map[string]string{"X-Test": "first", "x-test": "second"}
		}},
		{name: "unsupported api", mutate: func(request *domain.UpsertModelConfigRequest) { request.API = "custom-api" }},
		{name: "relative base url", mutate: func(request *domain.UpsertModelConfigRequest) { request.BaseURL = "/v1" }},
		{name: "max exceeds context", mutate: func(request *domain.UpsertModelConfigRequest) { request.MaxTokens = request.ContextWindow + 1 }},
		{name: "negative reserve tokens", mutate: func(request *domain.UpsertModelConfigRequest) { value := -1; request.ReserveTokens = &value }},
		{name: "zero image width", mutate: func(request *domain.UpsertModelConfigRequest) { value := 0; request.ImageMaxWidth = &value }},
		{name: "invalid JPEG quality", mutate: func(request *domain.UpsertModelConfigRequest) { value := 101; request.ImageJPEGQuality = &value }},
		{name: "invalid compat", mutate: func(request *domain.UpsertModelConfigRequest) { request.ModelCompatJSON = "[]" }},
		{name: "trailing compat data", mutate: func(request *domain.UpsertModelConfigRequest) { request.ModelCompatJSON = `{} {}` }},
		{name: "invalid thinking level key", mutate: func(request *domain.UpsertModelConfigRequest) { request.ThinkingLevelMapJSON = `{"turbo":"turbo"}` }},
		{name: "invalid thinking level value", mutate: func(request *domain.UpsertModelConfigRequest) { request.ThinkingLevelMapJSON = `{"max":false}` }},
		{name: "trailing thinking level data", mutate: func(request *domain.UpsertModelConfigRequest) { request.ThinkingLevelMapJSON = `{"max":"max"} {}` }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.mutate(&request)
			if _, err := service.UpsertModel(request); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestDeleteModelPreservesProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	fixture := `{"providers":{"proxy":{"baseUrl":"https://example.com/v1","future":true,"models":[{"id":"one"},{"id":"two"}]}}}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	settingsPath := filepath.Join(filepath.Dir(path), "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"compaction":{"modelOverrides":{"proxy/one":{"reserveTokens":1000},"proxy/two":{"reserveTokens":2000}}}}`), 0o600); err != nil {
		t.Fatalf("write settings fixture: %v", err)
	}
	service := newModelConfigService(path, nil)

	snapshot, err := service.DeleteModel(domain.DeleteModelConfigRequest{ProviderID: "proxy", ModelID: "one"})
	if err != nil {
		t.Fatalf("DeleteModel returned an error: %v", err)
	}
	if len(snapshot.Providers) != 1 || len(snapshot.Providers[0].Models) != 1 || snapshot.Providers[0].Models[0].ID != "two" {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"future": true`) {
		t.Fatalf("provider metadata was not preserved:\n%s", data)
	}
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(settings), `"proxy/one"`) || !strings.Contains(string(settings), `"proxy/two"`) {
		t.Fatalf("deleted model compaction override was not cleaned up:\n%s", settings)
	}
}

func TestUpsertModelPreservesUnknownPi87FieldsWhenRenaming(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "models.json")
	models := `{"providers":{"proxy":{"models":[{"id":"old/model","contextWindow":128000,"maxTokens":16000,"input":["text","image"],"inputLimits":{"images":{"maxPerMessage":8,"resize":{"maxWidth":2000,"futureCodec":"webp"}}}}]}}}`
	settings := `{"theme":"dark","compaction":{"enabled":true,"reserveTokens":16384,"modelOverrides":{"proxy/old/model":{"reserveTokens":12000,"future":true}}}}`
	if err := os.WriteFile(path, []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "settings.json"), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newModelConfigService(path, nil)
	snapshot, err := service.GetModelsConfig()
	if err != nil {
		t.Fatal(err)
	}
	model := snapshot.Providers[0].Models[0]
	if model.ReserveTokens == nil || *model.ReserveTokens != 12000 || model.ImageMaxWidth == nil || *model.ImageMaxWidth != 2000 {
		t.Fatalf("existing Pi 0.87 fields were not loaded: %#v", model)
	}

	keepRecentTokens, imageMaxWidth := 6000, 1568
	_, err = service.UpsertModel(domain.UpsertModelConfigRequest{
		OriginalProviderID: "proxy", OriginalModelID: "old/model", ProviderID: "renamed",
		ModelID: "new/model", ContextWindow: 128000, MaxTokens: 16000, ImageInput: true,
		KeepRecentTokens: &keepRecentTokens, ImageMaxWidth: &imageMaxWidth,
	})
	if err != nil {
		t.Fatal(err)
	}
	modelData, _ := os.ReadFile(path)
	settingsData, _ := os.ReadFile(filepath.Join(directory, "settings.json"))
	for _, expected := range []string{`"maxPerMessage": 8`, `"futureCodec": "webp"`, `"maxWidth": 1568`} {
		if !strings.Contains(string(modelData), expected) {
			t.Fatalf("models.json lost %s:\n%s", expected, modelData)
		}
	}
	for _, expected := range []string{`"theme": "dark"`, `"enabled": true`, `"renamed/new/model"`, `"keepRecentTokens": 6000`, `"future": true`} {
		if !strings.Contains(string(settingsData), expected) {
			t.Fatalf("settings.json lost %s:\n%s", expected, settingsData)
		}
	}
	if strings.Contains(string(settingsData), `"proxy/old/model"`) || strings.Contains(string(settingsData), `"reserveTokens": 12000`) {
		t.Fatalf("old compaction override was not migrated cleanly:\n%s", settingsData)
	}
}

func TestUpsertModelRenamesEntireProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	fixture := `{"providers":{"old":{"future":true,"apiKey":"visible","models":[{"id":"one"},{"id":"two"}]}}}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	service := newModelConfigService(path, nil)

	snapshot, err := service.UpsertModel(domain.UpsertModelConfigRequest{
		OriginalProviderID: "old", OriginalModelID: "one", ProviderID: "renamed",
		API: "openai-completions", APIKey: "visible", ModelID: "one",
		ContextWindow: 128000, MaxTokens: 16000,
	})
	if err != nil {
		t.Fatalf("UpsertModel returned an error: %v", err)
	}
	if len(snapshot.Providers) != 1 || snapshot.Providers[0].ID != "renamed" || len(snapshot.Providers[0].Models) != 2 {
		t.Fatalf("provider was not renamed with all models: %#v", snapshot)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), `"old"`) || !strings.Contains(string(data), `"future": true`) {
		t.Fatalf("provider rename did not preserve fields:\n%s", data)
	}
}

func TestAddModelsWritesDiscoveredSetAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	fixture := `{"providers":{"old":{"future":true,"models":[{"id":"existing"}]}}}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	service := newModelConfigService(path, nil)

	snapshot, err := service.AddModels(domain.AddModelsConfigRequest{
		OriginalProviderID: "old",
		ProviderID:         "renamed",
		BaseURL:            "https://gateway.example.com/v1",
		API:                "openai-completions",
		APIKey:             "visible-key",
		ProviderCompatJSON: `{"supportsDeveloperRole":false}`,
		Models: []domain.ManagedModel{
			{ID: "gpt-a", Name: "GPT A", ContextWindow: 272000, MaxTokens: 128000, Reasoning: true, ImageInput: true, ThinkingLevelMapJSON: `{"xhigh":"xhigh","max":"max"}`},
			{ID: "gpt-b", ContextWindow: 128000, MaxTokens: 16000, CompatJSON: `{"thinkingFormat":"deepseek"}`},
		},
	})
	if err != nil {
		t.Fatalf("AddModels returned an error: %v", err)
	}
	if len(snapshot.Providers) != 1 || snapshot.Providers[0].ID != "renamed" || len(snapshot.Providers[0].Models) != 3 {
		t.Fatalf("unexpected batch snapshot: %#v", snapshot)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read models.json: %v", err)
	}
	text := string(data)
	if strings.Contains(text, `"old"`) || !strings.Contains(text, `"future": true`) || !strings.Contains(text, `"gpt-b"`) || !strings.Contains(text, `"thinkingLevelMap"`) {
		t.Fatalf("batch write did not preserve or add expected fields:\n%s", data)
	}
}

func TestAddModelsRejectsExistingModelWithoutChangingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	fixture := `{"providers":{"proxy":{"models":[{"id":"existing"}]}}}`
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	service := newModelConfigService(path, nil)

	_, err := service.AddModels(domain.AddModelsConfigRequest{
		ProviderID: "proxy", API: "openai-completions", Models: []domain.ManagedModel{
			{ID: "existing", ContextWindow: 128000, MaxTokens: 16000},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate model error, got %v", err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"models":[{"id":"existing"}]`) {
		t.Fatalf("duplicate rejection changed the file: %s", data)
	}
}
