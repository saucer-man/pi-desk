package remotessh

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedHelperArtifactBundleIfPresent(t *testing.T) {
	root := filepath.Join("..", "..", "build", "remote-helper", "artifacts")
	manifestContent, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skip("generated helper artifact bundle is not present")
	}
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseHelperManifest(manifestContent)
	if err != nil {
		t.Fatalf("parse generated helper manifest: %v", err)
	}
	for _, artifact := range manifest.Artifacts {
		filename := "helper-" + artifact.OS + "-" + artifact.Architecture
		content, err := os.ReadFile(filepath.Join(root, filename))
		if err != nil {
			t.Fatalf("read generated %s: %v", filename, err)
		}
		if err := artifact.VerifyContent(content); err != nil {
			t.Fatalf("verify generated %s: %v", filename, err)
		}
	}
}
