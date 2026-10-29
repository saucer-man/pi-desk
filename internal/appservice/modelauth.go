package appservice

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"pi-desk/internal/domain"
	"pi-desk/internal/piruntime"
	"pi-desk/internal/processutil"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed resources/pi-desk-models.mjs
var modelBridgeSource string

func (service *ModelConfigService) ServiceStartup(context.Context, application.ServiceOptions) error {
	service.emitAuth = func(event domain.ModelAuthEvent) { application.Get().Event.Emit("models:auth", event) }
	return nil
}

func (service *ModelConfigService) ServiceShutdown() error {
	service.authMu.Lock()
	defer service.authMu.Unlock()
	if service.authCancel != nil {
		service.authCancel()
	}
	return nil
}

func (service *ModelConfigService) GetModelCatalog(ctx context.Context, refresh bool) (domain.ModelCatalogSnapshot, error) {
	return service.modelOperation(ctx, map[string]any{"operation": "catalog", "refresh": refresh}, "")
}

func (service *ModelConfigService) LoginModelProvider(parent context.Context, request domain.ModelAuthRequest) (domain.ModelCatalogSnapshot, error) {
	if err := validateIdentifier("provider id", request.ProviderID); err != nil {
		return domain.ModelCatalogSnapshot{}, err
	}
	if err := validateIdentifier("operation id", request.OperationID); err != nil {
		return domain.ModelCatalogSnapshot{}, err
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	service.authMu.Lock()
	if service.authCancel != nil {
		service.authMu.Unlock()
		return domain.ModelCatalogSnapshot{}, errors.New("another provider login is already running")
	}
	service.authOperationID, service.authCancel = request.OperationID, cancel
	service.authMu.Unlock()
	defer func() {
		service.authMu.Lock()
		service.authOperationID, service.authInput, service.authCancel = "", nil, nil
		service.authMu.Unlock()
	}()
	return service.modelOperation(ctx, map[string]any{"operation": "login", "providerId": request.ProviderID}, request.OperationID)
}

func (service *ModelConfigService) ReplyModelLogin(request domain.ModelAuthReply) error {
	if len(request.Value) > maxCredentialBytes {
		return errors.New("login response is too large")
	}
	service.authMu.Lock()
	defer service.authMu.Unlock()
	if service.authOperationID != request.OperationID || service.authInput == nil {
		return errors.New("this login is no longer active")
	}
	return json.NewEncoder(service.authInput).Encode(request)
}

func (service *ModelConfigService) SaveModelAPIKey(ctx context.Context, request domain.ModelAuthRequest) (domain.ModelCatalogSnapshot, error) {
	if err := validateIdentifier("provider id", request.ProviderID); err != nil {
		return domain.ModelCatalogSnapshot{}, err
	}
	if strings.TrimSpace(request.APIKey) == "" || len(request.APIKey) > maxCredentialBytes || strings.ContainsAny(request.APIKey, "\r\n\x00") {
		return domain.ModelCatalogSnapshot{}, errors.New("API key is empty or invalid")
	}
	return service.modelOperation(ctx, map[string]any{"operation": "api_key", "providerId": request.ProviderID, "apiKey": strings.TrimSpace(request.APIKey)}, "")
}

func (service *ModelConfigService) LogoutModelProvider(ctx context.Context, providerID string) (domain.ModelCatalogSnapshot, error) {
	if err := validateIdentifier("provider id", providerID); err != nil {
		return domain.ModelCatalogSnapshot{}, err
	}
	return service.modelOperation(ctx, map[string]any{"operation": "logout", "providerId": providerID}, "")
}

func (service *ModelConfigService) SetDefaultModel(ctx context.Context, request domain.DefaultModelRequest) (domain.ModelCatalogSnapshot, error) {
	if request.ProviderID != "" || request.ModelID != "" {
		if err := validateIdentifier("provider id", request.ProviderID); err != nil {
			return domain.ModelCatalogSnapshot{}, err
		}
		if err := validateIdentifier("model id", request.ModelID); err != nil {
			return domain.ModelCatalogSnapshot{}, err
		}
	}
	return service.modelOperation(ctx, map[string]any{"operation": "default", "providerId": request.ProviderID, "modelId": request.ModelID}, "")
}

func (service *ModelConfigService) modelOperation(parent context.Context, request map[string]any, operationID string) (domain.ModelCatalogSnapshot, error) {
	if service.pathErr != nil {
		return domain.ModelCatalogSnapshot{}, service.pathErr
	}
	ctx := parent
	if operationID == "" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, 25*time.Second)
		defer cancel()
	}
	piPath, err := piruntime.NewLocator().Resolve()
	if err != nil {
		return domain.ModelCatalogSnapshot{}, errors.New("Pi CLI was not found in PATH")
	}
	command, err := modelBridgeCommand(ctx, piPath, filepath.Dir(service.modelsPath))
	if err != nil {
		return domain.ModelCatalogSnapshot{}, err
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return domain.ModelCatalogSnapshot{}, err
	}
	defer stdin.Close()
	stdout, err := command.StdoutPipe()
	if err != nil {
		return domain.ModelCatalogSnapshot{}, err
	}
	if err := command.Start(); err != nil {
		return domain.ModelCatalogSnapshot{}, errors.New("could not start Pi model management")
	}
	if operationID != "" {
		service.authMu.Lock()
		service.authInput = stdin
		service.authMu.Unlock()
	}
	if err := json.NewEncoder(stdin).Encode(request); err != nil {
		_ = processutil.TerminateTree(command)
		_ = command.Wait()
		return domain.ModelCatalogSnapshot{}, err
	}
	var result domain.ModelCatalogSnapshot
	var bridgeErr error
	complete := false
	scanner := bufio.NewScanner(io.LimitReader(stdout, 8<<20))
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var envelope struct {
			Type  string                      `json:"type"`
			Data  domain.ModelCatalogSnapshot `json:"data"`
			Event domain.ModelAuthEvent       `json:"event"`
			Error string                      `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &envelope) != nil {
			continue
		}
		switch envelope.Type {
		case "event":
			if operationID != "" && service.emitAuth != nil {
				envelope.Event.OperationID = operationID
				service.emitAuth(envelope.Event)
			}
		case "result":
			result, complete = envelope.Data, true
		case "error":
			bridgeErr = errors.New(envelope.Error)
		}
	}
	if scanner.Err() != nil {
		_ = processutil.TerminateTree(command)
	}
	waitErr := command.Wait()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if bridgeErr != nil {
		return result, bridgeErr
	}
	if waitErr != nil || !complete || scanner.Err() != nil {
		return result, errors.New("Pi model management failed; check the installed Pi version and configuration")
	}
	return result, nil
}

func modelBridgeCommand(ctx context.Context, piPath, agentDir string) (*exec.Cmd, error) {
	node, err := exec.LookPath("node")
	if sibling := filepath.Join(filepath.Dir(piPath), "node.exe"); filepath.Ext(piPath) == ".cmd" {
		if info, statErr := os.Stat(sibling); statErr == nil && info.Mode().IsRegular() {
			node, err = sibling, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("Node.js is required by Pi model management: %w", err)
	}
	invocation := piruntime.Invocation{Executable: node, Args: []string{"--input-type=module", "-e", modelBridgeSource, piPath, agentDir}}
	command := invocation.CommandContext(ctx)
	command.Cancel = func() error { return processutil.TerminateTree(command) }
	command.WaitDelay = time.Second
	command.Env = append(os.Environ(), "PI_CODING_AGENT_DIR="+agentDir)
	return command, nil
}
