package sessionindex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLivePiSessionIndex(t *testing.T) {
	if os.Getenv("PI_DESK_LIVE_TEST") != "1" {
		t.Skip("set PI_DESK_LIVE_TEST=1 to scan the local Pi session catalog")
	}
	root, err := DefaultRoot()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	summaries, err := New(root).List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	rootKey := pathKey(absoluteRoot) + string(filepath.Separator)
	for _, summary := range summaries {
		absolutePath, err := filepath.Abs(summary.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(pathKey(absolutePath), rootKey) {
			t.Fatalf("session escaped the configured root: %s", summary.Path)
		}
	}
	t.Logf("indexed %d Pi sessions from %s", len(summaries), root)
}
