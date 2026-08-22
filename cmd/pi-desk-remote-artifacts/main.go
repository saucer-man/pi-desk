package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"pi-desk/internal/remoteprotocol"
	"pi-desk/internal/remotessh"
)

type target struct {
	goos         string
	architecture string
}

var targets = []target{
	{goos: "linux", architecture: "amd64"},
	{goos: "linux", architecture: "arm64"},
	{goos: "darwin", architecture: "amd64"},
	{goos: "darwin", architecture: "arm64"},
}

func main() {
	output := flag.String("output", "build/remote-helper/artifacts", "artifact output directory")
	buildIdentity := flag.String("build-identity", "", "helper hello build identity")
	piMin := flag.String("pi-min", "0.84.2", "inclusive Pi compatibility version")
	piMax := flag.String("pi-max", "0.85.0", "exclusive Pi compatibility version")
	flag.Parse()
	if flag.NArg() != 0 || strings.TrimSpace(*output) == "" || strings.TrimSpace(*buildIdentity) == "" {
		fatal("output and build-identity are required")
	}
	validationArtifact := remotessh.HelperArtifact{
		ProtocolVersion: remoteprotocol.Version,
		OS:              "linux",
		Architecture:    "amd64",
		Size:            1,
		SHA256:          strings.Repeat("0", sha256.Size*2),
		BuildIdentity:   *buildIdentity,
		PiVersionMin:    *piMin,
		PiVersionMax:    *piMax,
	}
	if err := validationArtifact.Validate(); err != nil {
		fatal("validate build metadata: %v", err)
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		fatal("create artifact output: %v", err)
	}

	manifest := remotessh.HelperManifest{Version: 1}
	for _, target := range targets {
		filename := "helper-" + target.goos + "-" + target.architecture
		destination := filepath.Join(*output, filename)
		temporary := destination + ".tmp"
		_ = os.Remove(temporary)
		command := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-s -w -X main.buildHash="+*buildIdentity, "-o", temporary, "./cmd/pi-desk-remote-helper")
		command.Env = buildEnvironment(target.goos, target.architecture)
		command.Stdout = os.Stdout
		command.Stderr = os.Stderr
		if err := command.Run(); err != nil {
			_ = os.Remove(temporary)
			fatal("build %s/%s helper: %v", target.goos, target.architecture, err)
		}
		content, err := os.ReadFile(temporary)
		if err != nil {
			_ = os.Remove(temporary)
			fatal("read %s/%s helper: %v", target.goos, target.architecture, err)
		}
		digest := sha256.Sum256(content)
		artifact := remotessh.HelperArtifact{
			ProtocolVersion: remoteprotocol.Version,
			OS:              target.goos,
			Architecture:    target.architecture,
			Size:            int64(len(content)),
			SHA256:          hex.EncodeToString(digest[:]),
			BuildIdentity:   *buildIdentity,
			PiVersionMin:    *piMin,
			PiVersionMax:    *piMax,
		}
		if err := artifact.Validate(); err != nil {
			_ = os.Remove(temporary)
			fatal("validate %s/%s artifact: %v", target.goos, target.architecture, err)
		}
		if err := replaceFile(temporary, destination); err != nil {
			fatal("publish %s/%s helper: %v", target.goos, target.architecture, err)
		}
		manifest.Artifacts = append(manifest.Artifacts, artifact)
	}
	if err := manifest.Validate(); err != nil {
		fatal("validate helper manifest: %v", err)
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fatal("encode helper manifest: %v", err)
	}
	encoded = append(encoded, '\n')
	manifestPath := filepath.Join(*output, "manifest.json")
	temporaryManifest := manifestPath + ".tmp"
	if err := os.WriteFile(temporaryManifest, encoded, 0o644); err != nil {
		fatal("write helper manifest: %v", err)
	}
	if err := replaceFile(temporaryManifest, manifestPath); err != nil {
		fatal("publish helper manifest: %v", err)
	}
}

func buildEnvironment(goos, architecture string) []string {
	environment := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		name, _, ok := strings.Cut(entry, "=")
		if ok && (strings.EqualFold(name, "GOOS") || strings.EqualFold(name, "GOARCH") || strings.EqualFold(name, "CGO_ENABLED")) {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "GOOS="+goos, "GOARCH="+architecture, "CGO_ENABLED=0")
}

func replaceFile(source, destination string) error {
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}

func fatal(format string, values ...any) {
	fmt.Fprintf(os.Stderr, "pi-desk remote artifacts: "+format+"\n", values...)
	os.Exit(1)
}
