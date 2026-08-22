package remotessh

import (
	"errors"
	"io/fs"
	"path"
	"strings"
)

// HelperArtifactBundle reads only manifest-selected helper files from the
// immutable application bundle. It has no fallback path or remote download.
type HelperArtifactBundle struct {
	filesystem fs.FS
	root       string
	manifest   HelperManifest
}

func NewHelperArtifactBundle(filesystem fs.FS, root string) (*HelperArtifactBundle, error) {
	root = strings.Trim(path.Clean(root), "/")
	if filesystem == nil || root == "" || root == "." {
		return nil, errors.New("remote helper artifact bundle is invalid")
	}
	content, err := fs.ReadFile(filesystem, path.Join(root, "manifest.json"))
	if err != nil {
		return nil, ErrHelperManifestInvalid
	}
	manifest, err := ParseHelperManifest(content)
	if err != nil {
		return nil, err
	}
	return &HelperArtifactBundle{filesystem: filesystem, root: root, manifest: manifest}, nil
}

func (bundle *HelperArtifactBundle) Select(goos, architecture, piVersion string) (HelperArtifact, []byte, error) {
	if bundle == nil || bundle.filesystem == nil {
		return HelperArtifact{}, nil, ErrHelperArtifactUnsupported
	}
	artifact, err := bundle.manifest.SelectHelperArtifact(goos, architecture, piVersion)
	if err != nil {
		return HelperArtifact{}, nil, err
	}
	content, err := fs.ReadFile(bundle.filesystem, path.Join(bundle.root, "helper-"+artifact.OS+"-"+artifact.Architecture))
	if err != nil {
		return HelperArtifact{}, nil, ErrHelperArtifactIntegrity
	}
	if err := artifact.VerifyContent(content); err != nil {
		return HelperArtifact{}, nil, err
	}
	return artifact, content, nil
}
