package remotessh

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHelperArtifactBundleSelectsAndVerifiesExactTarget(t *testing.T) {
	contents := map[string][]byte{
		"linux/amd64": []byte("linux-amd64"), "linux/arm64": []byte("linux-arm64"),
		"darwin/amd64": []byte("darwin-amd64"), "darwin/arm64": []byte("darwin-arm64"),
	}
	manifest := HelperManifest{Version: helperManifestVersion}
	files := fstest.MapFS{}
	for target, content := range contents {
		osName, architecture, _ := strings.Cut(target, "/")
		artifact := helperArtifactForTest(osName, architecture, content)
		manifest.Artifacts = append(manifest.Artifacts, artifact)
		files["artifacts/helper-"+osName+"-"+architecture] = &fstest.MapFile{Data: content}
	}
	manifestContent, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	files["artifacts/manifest.json"] = &fstest.MapFile{Data: manifestContent}
	bundle, err := NewHelperArtifactBundle(files, "artifacts")
	if err != nil {
		t.Fatal(err)
	}
	artifact, content, err := bundle.Select("linux", "amd64", "0.84.2")
	if err != nil || artifact.OS != "linux" || artifact.Architecture != "amd64" || string(content) != "linux-amd64" {
		t.Fatalf("artifact=%#v content=%q err=%v", artifact, content, err)
	}
	files["artifacts/helper-linux-amd64"].Data = []byte("tampered")
	if _, _, err := bundle.Select("linux", "amd64", "0.84.2"); err != ErrHelperArtifactIntegrity {
		t.Fatalf("tampered bundle error=%v", err)
	}
}
