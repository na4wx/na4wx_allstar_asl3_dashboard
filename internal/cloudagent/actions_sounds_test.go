package cloudagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"hamvoipconfiggui-asl3/internal/sounds"
)

// fakeSoxTool mirrors internal/sounds's own fakeSox test double: a
// stand-in for sox that always exits 0. Handles both invocation shapes
// this package's actions produce: Upload's (last arg is a destination
// path to create) and Preview's (last arg is "-", meaning "write WAV
// bytes to stdout") -- so both actionSoundsUpload and
// actionSoundsPreview can be exercised without a real sox binary or a
// real audio file.
func fakeSoxTool(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-sox")
	script := `#!/bin/sh
eval "last=\${$#}"
if [ "$last" = "-" ]; then
  printf 'FAKEWAVBYTES'
else
  touch "$last"
fi
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake sox: %v", err)
	}
	return path
}

func newSoundsTestAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	customDir := t.TempDir()
	stockDir := t.TempDir()
	soundsStore := sounds.New(customDir, stockDir, fakeSoxTool(t))
	a := New(t.TempDir()+"/settings.json", "", nil, "asterisk", soundsStore, nil, "", "", "", "")
	return a, customDir
}

func TestActionSoundsListAllEmptyIsNotError(t *testing.T) {
	a, _ := newSoundsTestAgent(t)
	result, err := a.dispatch(context.Background(), "sounds.listAll", nil)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	files, ok := result.([]sounds.File)
	if !ok {
		t.Fatalf("result type = %T, want []sounds.File", result)
	}
	if len(files) != 0 {
		t.Fatalf("files = %+v, want empty", files)
	}
}

func TestActionSoundsUploadAndDelete(t *testing.T) {
	a, customDir := newSoundsTestAgent(t)

	params, _ := json.Marshal(map[string]string{
		"name":       "test-upload",
		"dataBase64": base64.StdEncoding.EncodeToString([]byte("fake wav bytes")),
	})
	if _, err := a.dispatch(context.Background(), "sounds.upload", params); err != nil {
		t.Fatalf("upload error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(customDir, "test-upload.ulaw")); err != nil {
		t.Fatalf("expected test-upload.ulaw to exist: %v", err)
	}

	result, err := a.dispatch(context.Background(), "sounds.listAll", nil)
	if err != nil {
		t.Fatalf("list error = %v", err)
	}
	found := false
	for _, f := range result.([]sounds.File) {
		if f.Name == "test-upload" {
			found = true
		}
	}
	if !found {
		t.Fatal("uploaded file not found in listAll")
	}

	deleteParams, _ := json.Marshal(map[string]string{"name": "test-upload"})
	if _, err := a.dispatch(context.Background(), "sounds.delete", deleteParams); err != nil {
		t.Fatalf("delete error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(customDir, "test-upload.ulaw")); !os.IsNotExist(err) {
		t.Error("expected the file to be gone after delete")
	}
}

func TestActionSoundsUploadRejectsBadBase64(t *testing.T) {
	a, _ := newSoundsTestAgent(t)
	params, _ := json.Marshal(map[string]string{"name": "x", "dataBase64": "not-valid-base64!!"})
	if _, err := a.dispatch(context.Background(), "sounds.upload", params); err == nil {
		t.Fatal("dispatch() error = nil, want an error for invalid base64")
	}
}

func TestActionSoundsPreview(t *testing.T) {
	a, _ := newSoundsTestAgent(t)

	uploadParams, _ := json.Marshal(map[string]string{
		"name":       "test-preview",
		"dataBase64": base64.StdEncoding.EncodeToString([]byte("fake wav bytes")),
	})
	if _, err := a.dispatch(context.Background(), "sounds.upload", uploadParams); err != nil {
		t.Fatalf("upload error = %v", err)
	}

	previewParams, _ := json.Marshal(map[string]string{"name": "test-preview"})
	result, err := a.dispatch(context.Background(), "sounds.preview", previewParams)
	if err != nil {
		t.Fatalf("dispatch error = %v", err)
	}
	m, ok := result.(map[string]string)
	if !ok {
		t.Fatalf("result type = %T, want map[string]string", result)
	}
	wav, err := base64.StdEncoding.DecodeString(m["dataBase64"])
	if err != nil {
		t.Fatalf("dataBase64 does not decode: %v", err)
	}
	if len(wav) == 0 {
		t.Error("decoded WAV bytes are empty")
	}
}

func TestActionSoundsPreviewUnknownName(t *testing.T) {
	a, _ := newSoundsTestAgent(t)
	params, _ := json.Marshal(map[string]string{"name": "does-not-exist"})
	if _, err := a.dispatch(context.Background(), "sounds.preview", params); err == nil {
		t.Fatal("dispatch() error = nil, want an error for a nonexistent sound")
	}
}
