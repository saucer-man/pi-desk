package sessionindex

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const mutationHeader = `{"type":"session","version":3,"id":"test","cwd":"D:/repo"}`

func TestMutationRejectsAmbiguousOrBrokenTrees(t *testing.T) {
	for _, invalid := range []string{
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Duplicate"}}`,
		`{"type":"message","id":"bad","parentId":"missing"}`,
		`{"type":"message","id":"bad","parentId":"bad"}`,
		`{"type":"message","id":"a","parentId":"b"}` + "\n" + `{"type":"message","id":"b","parentId":"a"}`,
		`null`, `[]`, `{"type":"message","parentId":null}`,
	} {
		for _, operation := range []string{"edit", "delete", "replay", "fork"} {
			t.Run(operation+invalid, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "session.jsonl")
				original := mutationHeader + "\n" + `{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}` + "\n" + invalid + "\n"
				writeSession(t, path, original)
				index := New(root)
				var err error
				switch operation {
				case "edit":
					_, err = index.EditMessage(path, "root", "changed")
				case "delete":
					_, err = index.DeleteMessage(path, "root")
				case "replay":
					_, err = index.RewindBefore(path, "root")
				case "fork":
					_, err = index.ForkAt(path, "root")
				}
				if err == nil {
					t.Fatal("broken tree accepted")
				}
				data, _ := os.ReadFile(path)
				files, _ := os.ReadDir(root)
				if string(data) != original || len(files) != 1 {
					t.Fatal("invalid input caused a write")
				}
			})
		}
	}
}

func TestReplayPreservesOtherBranchesAndRestoresParentAsLeaf(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "sibling", true: "shared-turn"}[shared], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "session.jsonl")
			sideParent := "before"
			if shared {
				sideParent = "latest"
			}
			writeSession(t, path, strings.Join([]string{
				mutationHeader,
				`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
				`{"type":"message","id":"before","parentId":"root","message":{"role":"assistant","content":"Before"}}`,
				`{"type":"message","id":"latest","parentId":"before","message":{"role":"user","content":"Latest"}}`,
				`{"type":"message","id":"side","parentId":"` + sideParent + `","message":{"role":"user","content":"Other branch"}}`,
				`{"type":"message","id":"answer","parentId":"latest","message":{"role":"assistant","content":"Answer"}}`,
			}, "\n")+"\n")
			index := New(root)
			if _, err := index.RewindBefore(path, "latest"); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(path)
			if !strings.Contains(string(data), `"id":"side"`) || strings.Contains(string(data), `"id":"answer"`) {
				t.Fatalf("wrong branch removed: %s", data)
			}
			messages, err := index.Messages(path)
			if err != nil || !slices.Equal(messageContents(t, messages), []string{"Root", "Before"}) {
				t.Fatalf("wrong active history: %s %v", messages, err)
			}
			// The reordered file must remain editable and must not introduce duplicate IDs.
			if _, err := index.EditMessage(path, "before", "Updated"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReplayRootDoesNotEraseIndependentBranches(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	original := mutationHeader + "\n" + `{"type":"message","id":"other","parentId":null,"message":{"role":"user","content":"Other"}}` + "\n" + `{"type":"message","id":"latest","parentId":null,"message":{"role":"user","content":"Latest"}}` + "\n"
	writeSession(t, path, original)
	if _, err := New(root).RewindBefore(path, "latest"); err == nil {
		t.Fatal("root replay would erase another branch")
	}
	data, _ := os.ReadFile(path)
	if string(data) != original {
		t.Fatal("file was changed")
	}
}

func TestDeleteLeafDoesNotActivateAnUnrelatedBranch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	writeSession(t, path, strings.Join([]string{
		mutationHeader,
		`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
		`{"type":"message","id":"side","parentId":"root","message":{"role":"user","content":"Other branch"}}`,
		`{"type":"message","id":"latest","parentId":"root","message":{"role":"user","content":"Delete this"}}`,
	}, "\n")+"\n")
	index := New(root)
	if _, err := index.DeleteMessage(path, "latest"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"id":"side"`) {
		t.Fatal("unrelated branch was erased")
	}
	messages, err := index.Messages(path)
	if err != nil || !slices.Equal(messageContents(t, messages), []string{"Root"}) {
		t.Fatalf("wrong active history: %s %v", messages, err)
	}
}

func TestToolMutationCrossesMetadataAndRemovesDanglingLabels(t *testing.T) {
	for _, operation := range []string{"delete", "fork"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "session.jsonl")
			writeSession(t, path, strings.Join([]string{
				mutationHeader,
				`{"type":"message","id":"root","parentId":null,"message":{"role":"user","content":"Root"}}`,
				`{"type":"message","id":"assistant","parentId":"root","message":{"role":"assistant","content":[{"type":"toolCall","id":"call","name":"read"}]}}`,
				`{"type":"custom","id":"meta","parentId":"assistant","customType":"test"}`,
				`{"type":"message","id":"result","parentId":"meta","message":{"role":"toolResult","toolCallId":"call","content":"Result"}}`,
				`{"type":"label","id":"label","parentId":"result","targetId":"result","label":"Result label"}`,
				`{"type":"message","id":"later","parentId":"label","message":{"role":"assistant","content":"Later"}}`,
			}, "\n")+"\n")
			index := New(root)
			if operation == "delete" {
				if _, err := index.DeleteMessage(path, "assistant"); err != nil {
					t.Fatal(err)
				}
				data, _ := os.ReadFile(path)
				if strings.Contains(string(data), `"id":"result"`) || strings.Contains(string(data), `"id":"label"`) {
					t.Fatalf("orphan remains: %s", data)
				}
			} else {
				fork, err := index.ForkAt(path, "assistant")
				if err != nil {
					t.Fatal(err)
				}
				data, _ := os.ReadFile(fork)
				if !strings.Contains(string(data), `"id":"result"`) || strings.Contains(string(data), `"id":"later"`) {
					t.Fatalf("tool result missing or fork went too far: %s", data)
				}
			}
		})
	}
}

func TestRestoreRefusesToOverwriteNewEntries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	writeSession(t, path, mutationHeader+"\n"+`{"type":"message","id":"user","parentId":null,"message":{"role":"user","content":"Before"}}`+"\n")
	index := New(root)
	mutation, err := index.EditMessage(path, "user", "After")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	latest := string(data) + `{"type":"message","id":"new","parentId":"user","message":{"role":"assistant","content":"New"}}` + "\n"
	writeSession(t, path, latest)
	if err := index.RestoreMutation(mutation); err == nil {
		t.Fatal("stale backup restored")
	}
	data, _ = os.ReadFile(path)
	if string(data) != latest {
		t.Fatal("new content lost")
	}
}

func TestEditRemovesOldTextSignatureAndForkIncludesImages(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "session.jsonl")
	writeSession(t, path, mutationHeader+"\n"+`{"type":"message","id":"user","parentId":null,"message":{"role":"user","content":[{"type":"image","data":"aW1hZ2U=","mimeType":"image/png"}]}}`+"\n"+`{"type":"message","id":"assistant","parentId":"user","message":{"role":"assistant","content":[{"type":"text","text":"Before","textSignature":"old-signature"}]}}`+"\n")
	index := New(root)
	if _, err := index.EditMessage(path, "assistant", "After"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "textSignature") {
		t.Fatal("stale provider signature retained")
	}
	fork, err := index.ForkBefore(path, "user")
	if err != nil || len(fork.Images) != 1 || fork.Text != "" {
		t.Fatalf("fork=%+v error=%v", fork, err)
	}
}
