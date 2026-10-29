package appservice

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"pi-desk/internal/domain"
)

// A local SDK fixture tests the bridge contract without contacting an identity
// provider, using an account, or starting a model inference request.
func modelSDKFixture(t *testing.T) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node.js is unavailable")
	}
	root := t.TempDir()
	packageDir := filepath.Join(root, "node_modules", "@earendil-works", "pi-coding-agent")
	if err := os.MkdirAll(packageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"package.json": `{"name":"@earendil-works/pi-coding-agent","type":"module","main":"index.mjs"}`,
		"index.mjs": `
import fs from 'node:fs';
import path from 'node:path';
let agentDir;
const read = (file) => { try { return JSON.parse(fs.readFileSync(path.join(agentDir,file),'utf8')); } catch(e) { if(e.code==='ENOENT') return {}; throw e; } };
const write = (file,data) => fs.writeFileSync(path.join(agentDir,file),JSON.stringify(data));
export class SettingsManager {
  static create(cwd,dir) { agentDir=dir; return new SettingsManager(); }
  getDefaultProvider() { return read('settings.json').defaultProvider; }
  getDefaultModel() { return read('settings.json').defaultModel; }
  getOrCreateDeviceId() { return 'stable-test-device'; }
  setDefaultModelAndProvider(provider,model) { write('settings.json',{...read('settings.json'),defaultProvider:provider,defaultModel:model}); }
  async flush() {}
  drainErrors() { return []; }
}
export const readStoredCredential=(id)=>read('auth.json')[id];
export class ModelRuntime {
  static async create() { return new ModelRuntime(); }
  getError() { try { read('models.json'); return ''; } catch { return 'Invalid models.json'; } }
  async listCredentials() { return Object.entries(read('auth.json')).map(([providerId,c])=>({providerId,type:c.type})); }
  getProviders() { return [{id:'openai',name:'OpenAI',auth:{oauth:{login(){}},apiKey:{login(){}}}},{id:'native-test',name:'Native test',auth:{oauth:{login(){}}}}]; }
  getProviderAuthStatus(id) { return {configured:!!read('auth.json')[id],source:read('auth.json')[id]?'stored':undefined}; }
  getAvailableSnapshot() { return read('auth.json')['native-test']?[{id:'native-model',name:'Native model',provider:'native-test',contextWindow:12345,reasoning:true}]:[]; }
  isUsingOAuth() { return false; }
  async refresh() {}
  async login(id,type,interaction,options) {
    if(type!=='oauth'||options.agentName!=='Pi Desk'||options.getDeviceId()!=='stable-test-device') throw new Error('Incorrect native login options');
    interaction.notify({type:'auth_url',url:'https://example.test/authorize'});
    const value=await interaction.prompt({type:'manual_code',message:'Callback URL'});
    if(value!=='test-callback') throw new Error('Invalid test reply');
    write('auth.json',{...read('auth.json'),[id]:{type:'oauth',access:'hidden-access',refresh:'hidden-refresh',expires:9999999999999}});
  }
  async logout(id) { const data=read('auth.json'); delete data[id]; write('auth.json',data); }
}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(packageDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	piName := "pi"
	if runtime.GOOS == "windows" {
		piName = "pi.cmd"
	}
	piPath := filepath.Join(root, piName)
	if err := os.WriteFile(piPath, []byte("fixture"), 0o700); err != nil {
		t.Fatal(err)
	}
	return piPath, t.TempDir()
}

func invokeModelFixture(t *testing.T, piPath, directory, request, replies string) (domain.ModelCatalogSnapshot, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command, err := modelBridgeCommand(ctx, piPath, directory)
	if err != nil {
		t.Fatal(err)
	}
	command.Stdin = strings.NewReader(request + "\n" + replies)
	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("bridge failed: %v: %s", runErr, output)
	}
	var result domain.ModelCatalogSnapshot
	for _, line := range bytes.Split(output, []byte("\n")) {
		var envelope struct {
			Type string                      `json:"type"`
			Data domain.ModelCatalogSnapshot `json:"data"`
		}
		if json.Unmarshal(line, &envelope) == nil && envelope.Type == "result" {
			result = envelope.Data
		}
	}
	if result.AgentDir != directory {
		t.Fatalf("result did not identify the isolated agent directory: %s", output)
	}
	if bytes.Contains(output, []byte("hidden-access")) || bytes.Contains(output, []byte("hidden-refresh")) {
		t.Fatal("bridge returned credentials")
	}
	return result, string(output)
}

func TestModelBridgeLoginDefaultAndLogoutShareNativeStorage(t *testing.T) {
	piPath, directory := modelSDKFixture(t)
	if err := os.WriteFile(filepath.Join(directory, "auth.json"), []byte(`{"other":{"type":"api_key","key":"other-preserved"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "settings.json"), []byte(`{"custom":{"keep":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, output := invokeModelFixture(t, piPath, directory, `{"operation":"login","providerId":"native-test"}`, "{\"promptId\":1,\"value\":\"test-callback\"}\n")
	if len(result.Models) != 1 || result.Models[0].ID != "native-model" || !strings.Contains(output, `"type":"prompt"`) {
		t.Fatal("native login did not refresh its model catalog or expose the prompt")
	}
	result, _ = invokeModelFixture(t, piPath, directory, `{"operation":"default","providerId":"native-test","modelId":"native-model"}`, "")
	if result.DefaultProvider != "native-test" || result.DefaultModel != "native-model" {
		t.Fatal("default model pair was not persisted")
	}
	settings, _ := os.ReadFile(filepath.Join(directory, "settings.json"))
	if !bytes.Contains(settings, []byte(`"custom":{"keep":true}`)) {
		t.Fatal("unrelated settings were lost")
	}
	result, _ = invokeModelFixture(t, piPath, directory, `{"operation":"logout","providerId":"native-test"}`, "")
	if len(result.Models) != 0 || result.DefaultModel != "native-model" {
		t.Fatal("logout must keep the configured default while removing model availability")
	}
	auth, _ := os.ReadFile(filepath.Join(directory, "auth.json"))
	if !bytes.Contains(auth, []byte("other-preserved")) || bytes.Contains(auth, []byte("hidden-access")) {
		t.Fatal("logout changed another provider or retained the removed credential")
	}
	result, _ = invokeModelFixture(t, piPath, directory, `{"operation":"default","providerId":"","modelId":""}`, "")
	if result.DefaultModel != "" || result.DefaultProvider != "" {
		t.Fatal("automatic model selection was not restored")
	}
}

func TestModelCatalogFindsAuthorizedBuiltinsWithoutModelsJSON(t *testing.T) {
	piPath, directory := modelSDKFixture(t)
	t.Setenv("PATH", filepath.Dir(piPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(directory, "auth.json"), []byte(`{"native-test":{"type":"oauth","access":"hidden-access","refresh":"hidden-refresh"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newModelConfigService(filepath.Join(directory, "models.json"), nil)
	models, err := service.GetConfiguredModels()
	if err != nil || len(models) != 1 || models[0].Provider != "native-test" {
		t.Fatalf("authorized built-in was not discovered: %v", err)
	}
	encoded, _ := json.Marshal(models)
	if bytes.Contains(encoded, []byte("hidden-")) {
		t.Fatal("model selection leaked auth data")
	}
}

func TestModelBridgeCancellationStopsWaitingLogin(t *testing.T) {
	piPath, directory := modelSDKFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command, err := modelBridgeCommand(ctx, piPath, directory)
	if err != nil {
		t.Fatal(err)
	}
	stdin, _ := command.StdinPipe()
	stdout, _ := command.StdoutPipe()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	_, _ = stdin.Write([]byte("{\"operation\":\"login\",\"providerId\":\"native-test\"}\n"))
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || !strings.Contains(scanner.Text(), "auth_url") {
		t.Fatal("login did not reach browser authorization")
	}
	cancel()
	if command.Wait() == nil {
		t.Fatal("cancelled login unexpectedly succeeded")
	}
	if _, err := os.Stat(filepath.Join(directory, "auth.json")); !os.IsNotExist(err) {
		t.Fatal("cancelled login wrote a credential")
	}
}

func TestAccountCatalogFiltersBundledAndCustomModelEntries(t *testing.T) {
	piPath, directory := modelSDKFixture(t)
	sdkPath := filepath.Join(filepath.Dir(piPath), "node_modules", "@earendil-works", "pi-coding-agent", "index.mjs")
	file, err := os.OpenFile(sdkPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString(`
ModelRuntime.prototype.isUsingOAuth = () => true;
ModelRuntime.prototype.getAvailableSnapshot = () => [{provider:'openai',id:'allowed',name:'Allowed'},{provider:'openai',id:'blocked',name:'Blocked'}];
`)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "auth.json"), []byte(`{"openai":{"type":"oauth","access":"hidden-access","refresh":"hidden-refresh"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "models.json"), []byte(`{"providers":{"openai":{"models":[{"id":"blocked"}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(directory, "cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cache, _ := json.Marshal(map[string]any{"fingerprint": fmt.Sprintf("%x", sha256.Sum256([]byte("hidden-access"))), "updatedAt": time.Now().UnixMilli(), "models": []map[string]string{{"id": "allowed", "name": "Account model"}}})
	if err := os.WriteFile(filepath.Join(cacheDir, "pi-desk-openai-models.json"), cache, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(piPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	service := newModelConfigService(filepath.Join(directory, "models.json"), nil)
	models, err := service.GetConfiguredModels()
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "allowed" || models[0].Source != "account" {
		t.Fatal("bundled or custom entries bypassed the account catalog")
	}
	if models[0].Name != "Account model" {
		t.Fatal("account display name was not preserved")
	}
}

func TestSavedModelKeyIsOpaqueAndPreservedUntilExplicitRemoval(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"test":{"apiKey":"opaque-key","future":true,"models":[{"id":"model"}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	service := newModelConfigService(path, nil)
	request := domain.UpsertModelConfigRequest{OriginalProviderID: "test", OriginalModelID: "model", ProviderID: "test", ModelID: "model", ModelName: "Updated", ContextWindow: 128000, MaxTokens: 16000}
	snapshot, err := service.UpsertModel(request)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Providers[0].APIKey != "" || !snapshot.Providers[0].HasAPIKey {
		t.Fatal("saved key must be hidden")
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("opaque-key")) {
		t.Fatal("empty editor key erased the stored credential")
	}
	request.ClearAPIKey = true
	if _, err := service.UpsertModel(request); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if bytes.Contains(data, []byte("apiKey")) || !bytes.Contains(data, []byte(`"future": true`)) {
		t.Fatal("explicit removal lost unrelated configuration")
	}
}
