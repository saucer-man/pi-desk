package remotessh

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"pi-desk/internal/processutil"
	"pi-desk/internal/remotehelper"
	"pi-desk/internal/remoteprotocol"
)

var (
	ErrHelperLaunch           = errors.New("remote helper launch failed")
	ErrHelperHandshake        = errors.New("remote helper handshake failed")
	ErrHelperProtocolMismatch = errors.New("remote helper protocol or identity mismatch")
)

// HelperProbeResult is the bounded identity returned by an exact helper
// hello/ping/shutdown probe. It contains no remote path or process output.
type HelperProbeResult struct {
	ProtocolVersion uint16
	BuildIdentity   string
	OS              string
	Architecture    string
	Capabilities    []string
}

func (locator *Locator) helperInvocation(hostAlias string, artifact HelperArtifact) (Invocation, error) {
	if err := artifact.Validate(); err != nil {
		return Invocation{}, err
	}
	target, executable, err := locator.resolveTarget(hostAlias)
	if err != nil {
		return Invocation{}, err
	}
	// This is one of the two ADR-approved POSIX bootstrap templates. The only
	// substitutions are a validated integer protocol and lowercase SHA-256.
	// No target input, home path, workspace path, or command is interpolated.
	remoteCommand := `exec "$HOME/.cache/pi-desk/remote-helper/` + strconv.Itoa(int(artifact.ProtocolVersion)) + `/` + artifact.SHA256 + `/helper" serve-stdio`
	args := connectionPolicyArgs()
	args = append(args, "--", target.HostAlias, remoteCommand)
	return Invocation{Executable: executable, Args: locator.withTestConfig(args)}, nil
}

// ProbeInstalledHelper starts only the manifest-selected helper in the fixed
// cache, performs an exact version/build/platform/capability handshake and a
// ping, then asks it to shut down. Any launch or handshake failure revokes the
// generation before returning. Raw stderr is discarded after bounded in-memory
// classification.
func (installer *HelperInstaller) ProbeInstalledHelper(ctx context.Context, generation uint64, artifact HelperArtifact) (HelperProbeResult, error) {
	if err := artifact.Validate(); err != nil {
		return HelperProbeResult{}, err
	}
	result, err := installer.probeInstalledHelper(ctx, generation, artifact)
	if err != nil {
		installer.supervisor.Disconnect()
	}
	return result, err
}

func (installer *HelperInstaller) probeInstalledHelper(ctx context.Context, generation uint64, artifact HelperArtifact) (HelperProbeResult, error) {
	generationContext, release, err := installer.supervisor.bindGenerationContext(ctx, generation)
	if err != nil {
		return HelperProbeResult{}, err
	}
	defer release()
	invocation, err := installer.locator.helperInvocation(installer.supervisor.hostAlias, artifact)
	if err != nil {
		return HelperProbeResult{}, err
	}

	processContext, stopProcess := context.WithCancel(generationContext)
	defer stopProcess()
	command := exec.CommandContext(processContext, invocation.Executable, invocation.Args...)
	processutil.ConfigureBackground(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return HelperProbeResult{}, ErrHelperLaunch
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return HelperProbeResult{}, ErrHelperLaunch
	}
	stderr := &boundedOutput{limit: maxConnectionProbeStderrBytes}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return HelperProbeResult{}, helperProcessError(processContext, stderr, err)
	}
	waitChannel := make(chan error, 1)
	go func() {
		waitChannel <- command.Wait()
	}()
	waited := false
	defer func() {
		if !waited {
			stopProcess()
			<-waitChannel
		}
	}()
	classifyIOFailure := func() error {
		var classified error
		classified, waited = classifyHelperIOFailure(processContext, stderr, waitChannel)
		return classified
	}

	writer := remoteprotocol.NewWriter(stdin, remoteprotocol.Limits{})
	reader := remoteprotocol.NewReader(stdout, remoteprotocol.Limits{})
	nonce, err := helperProbeNonce()
	if err != nil {
		return HelperProbeResult{}, ErrHelperHandshake
	}
	requestID := "hello-" + nonce[:16]
	helloPayload, err := remoteprotocol.EncodePayload(remotehelper.HelloRequest{
		Nonce:         nonce,
		ClientVersion: "pi-desk-bootstrap-v1",
	})
	if err != nil {
		return HelperProbeResult{}, ErrHelperHandshake
	}
	if err := writer.Write(remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindRequest,
		ID:         requestID,
		Generation: generation,
		Method:     remotehelper.MethodHello,
		Payload:    helloPayload,
	}, nil); err != nil {
		return HelperProbeResult{}, classifyIOFailure()
	}
	helloFrame, err := reader.Read()
	if err != nil {
		return HelperProbeResult{}, classifyIOFailure()
	}
	var hello remotehelper.HelloResponse
	if err := validateHelperResponse(helloFrame, requestID, generation, &hello); err != nil {
		return HelperProbeResult{}, err
	}
	if hello.Nonce != nonce || hello.ProtocolVersion != artifact.ProtocolVersion || hello.BuildHash != artifact.BuildIdentity || hello.OS != artifact.OS || hello.Architecture != artifact.Architecture || !sameCapabilities(hello.Capabilities, requiredHelperCapabilities()) {
		return HelperProbeResult{}, ErrHelperProtocolMismatch
	}

	if err := writeEmptyHelperRequest(writer, "ping", generation, remotehelper.MethodPing); err != nil {
		return HelperProbeResult{}, classifyIOFailure()
	}
	pingFrame, err := reader.Read()
	if err != nil {
		return HelperProbeResult{}, classifyIOFailure()
	}
	var ping remotehelper.PingResponse
	if err := validateHelperResponse(pingFrame, "ping", generation, &ping); err != nil || !ping.OK {
		if err != nil {
			return HelperProbeResult{}, err
		}
		return HelperProbeResult{}, ErrHelperProtocolMismatch
	}

	if err := writeEmptyHelperRequest(writer, "shutdown", generation, remotehelper.MethodShutdown); err != nil {
		return HelperProbeResult{}, classifyIOFailure()
	}
	shutdownFrame, err := reader.Read()
	if err != nil {
		return HelperProbeResult{}, classifyIOFailure()
	}
	var shutdown struct{}
	if err := validateHelperResponse(shutdownFrame, "shutdown", generation, &shutdown); err != nil {
		return HelperProbeResult{}, err
	}
	_ = stdin.Close()
	waitErr := <-waitChannel
	waited = true
	if waitErr != nil || stderr.overflow {
		return HelperProbeResult{}, helperProcessError(processContext, stderr, waitErr)
	}
	if err := installer.supervisor.ValidateGeneration(generation); err != nil {
		return HelperProbeResult{}, err
	}
	capabilities := append([]string(nil), hello.Capabilities...)
	slices.Sort(capabilities)
	return HelperProbeResult{
		ProtocolVersion: hello.ProtocolVersion,
		BuildIdentity:   hello.BuildHash,
		OS:              hello.OS,
		Architecture:    hello.Architecture,
		Capabilities:    capabilities,
	}, nil
}

func validateHelperResponse(frame remoteprotocol.Frame, requestID string, generation uint64, output any) error {
	envelope := frame.Envelope
	if envelope.Kind == remoteprotocol.KindError {
		return ErrHelperHandshake
	}
	if envelope.Kind != remoteprotocol.KindResponse || envelope.ID != requestID || envelope.Generation != generation || len(frame.Blob) != 0 {
		return ErrHelperProtocolMismatch
	}
	if err := remoteprotocol.DecodePayload(envelope.Payload, output); err != nil {
		return ErrHelperProtocolMismatch
	}
	return nil
}

func writeEmptyHelperRequest(writer *remoteprotocol.Writer, requestID string, generation uint64, method string) error {
	return writer.Write(remoteprotocol.Envelope{
		Version:    remoteprotocol.Version,
		Kind:       remoteprotocol.KindRequest,
		ID:         requestID,
		Generation: generation,
		Method:     method,
	}, nil)
}

func helperProbeNonce() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func requiredHelperCapabilities() []string {
	return []string{
		remotehelper.MethodFileContent, remotehelper.MethodFileHash, remotehelper.MethodFileImage, remotehelper.MethodFileList,
		remotehelper.MethodFileMkdir, remotehelper.MethodFileRead, remotehelper.MethodFileStat, remotehelper.MethodFileWrite,
		remotehelper.MethodBashRun, remotehelper.MethodGitRead, remotehelper.MethodPing, remotehelper.MethodRootOpen, remotehelper.MethodSearchFind,
		remotehelper.MethodSearchGrep, remotehelper.MethodShutdown, remotehelper.MethodTerminalRun,
	}
}

func sameCapabilities(actual, expected []string) bool {
	actual = append([]string(nil), actual...)
	expected = append([]string(nil), expected...)
	slices.Sort(actual)
	slices.Sort(expected)
	return slices.Equal(actual, expected)
}

func classifyHelperIOFailure(ctx context.Context, stderr *boundedOutput, waitChannel <-chan error) (error, bool) {
	select {
	case waitErr := <-waitChannel:
		return helperProcessError(ctx, stderr, waitErr), true
	case <-ctx.Done():
		return helperProtocolError(ctx), false
	case <-time.After(250 * time.Millisecond):
		return helperProtocolError(ctx), false
	}
}

func helperProtocolError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return lifecycleError(FailureDisconnected, ReasonDisconnected, ErrConnectionGenerationRevoked)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return lifecycleError(FailureConnect, ReasonConnectionTimeout, ctx.Err())
	}
	return ErrHelperHandshake
}

func helperProcessError(ctx context.Context, stderr *boundedOutput, cause error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return lifecycleError(FailureDisconnected, ReasonDisconnected, ErrConnectionGenerationRevoked)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return lifecycleError(FailureConnect, ReasonConnectionTimeout, ctx.Err())
	}
	if stderr != nil && stderr.overflow {
		return lifecycleError(FailureOutputLimit, ReasonOutputLimit, ErrHelperLaunch)
	}
	if stderr != nil {
		value := strings.ToLower(stderr.buffer.String())
		if strings.Contains(value, "permission denied") || strings.Contains(value, "not found") {
			return ErrHelperArtifactUnsupported
		}
		failure := ClassifyOpenSSHFailure(stderr.buffer.Bytes())
		if failure.Reason != ReasonUnknown {
			return &ConnectionProbeError{Failure: failure, cause: fmt.Errorf("%w: %w", ErrHelperLaunch, cause)}
		}
	}
	return fmt.Errorf("%w: process exited", ErrHelperLaunch)
}
