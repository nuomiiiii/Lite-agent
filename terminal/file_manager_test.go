package terminal

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type recordingFileWriter struct {
	mu       sync.Mutex
	messages []map[string]any
}

func (writer *recordingFileWriter) writeJSON(value any) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	message, ok := value.(map[string]any)
	if ok {
		writer.messages = append(writer.messages, message)
	}
	return nil
}

func (writer *recordingFileWriter) last(t *testing.T) map[string]any {
	t.Helper()
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if len(writer.messages) == 0 {
		t.Fatal("expected a file manager response")
	}
	return writer.messages[len(writer.messages)-1]
}

func responseOK(t *testing.T, response map[string]any) bool {
	t.Helper()
	ok, exists := response["ok"].(bool)
	if !exists {
		t.Fatalf("response has no boolean ok field: %#v", response)
	}
	return ok
}

func uploadIDFromResponse(t *testing.T, response map[string]any) string {
	t.Helper()
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("response has no data: %#v", response)
	}
	id, ok := data["upload_id"].(string)
	if !ok || id == "" {
		t.Fatalf("response has no upload id: %#v", response)
	}
	return id
}

func TestSQLiteFilesUseNormalFileOperations(t *testing.T) {
	directory := t.TempDir()
	database := filepath.Join(directory, "lite.db")
	payload := append([]byte("SQLite format 3\x00"), make([]byte, 32)...)
	if err := os.WriteFile(database, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	writer := &recordingFileWriter{}
	manager := newFileManager(writer)
	defer manager.close()

	destination := filepath.Join(directory, "renamed.db")
	manager.rename(fileRequest{Type: "file.rename", ID: "rename", Path: database, Destination: destination})
	if !responseOK(t, writer.last(t)) {
		t.Fatalf("SQLite file rename failed: %#v", writer.last(t))
	}
	if content, err := os.ReadFile(destination); err != nil || string(content) != string(payload) {
		t.Fatalf("renamed SQLite file changed: content=%q err=%v", content, err)
	}
}

func TestFilesystemRootCannotBeRenamed(t *testing.T) {
	writer := &recordingFileWriter{}
	manager := newFileManager(writer)
	defer manager.close()

	manager.rename(fileRequest{
		Type:        "file.rename",
		ID:          "rename-root",
		Path:        filesystemRoots()[0],
		Destination: filepath.Join(t.TempDir(), "renamed-root"),
	})
	if responseOK(t, writer.last(t)) {
		t.Fatal("filesystem root was renamed")
	}
}

func TestCopyRegularFile(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.txt")
	destination := filepath.Join(directory, "destination.txt")
	if err := os.WriteFile(source, []byte("copied content"), 0o640); err != nil {
		t.Fatal(err)
	}
	writer := &recordingFileWriter{}
	manager := newFileManager(writer)
	defer manager.close()

	manager.handle([]byte(`{"type":"file.copy","id":"copy","path":"` + filepath.ToSlash(source) + `","destination":"` + filepath.ToSlash(destination) + `"}`))
	if !responseOK(t, writer.last(t)) {
		t.Fatalf("regular file copy failed: %#v", writer.last(t))
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "copied content" {
		t.Fatalf("copied content = %q", content)
	}
}

func TestCopyDirectoryRecursively(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	destination := filepath.Join(parent, "destination")
	if err := os.MkdirAll(filepath.Join(source, "nested"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "notes.txt"), []byte("nested"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := copyPath(source, destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(destination, "nested", "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "nested" {
		t.Fatalf("copied nested content = %q", content)
	}
}

func TestCopyRejectsUnsafeSources(t *testing.T) {
	parent := t.TempDir()
	regularDirectory := filepath.Join(parent, "regular-directory")
	if err := os.Mkdir(regularDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyPath(regularDirectory, filepath.Join(regularDirectory, "nested-copy")); err == nil {
		t.Fatal("directory was copied into itself")
	}

	regular := filepath.Join(parent, "regular.txt")
	if err := os.WriteFile(regular, []byte("regular"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(parent, "link.txt")
	if err := os.Symlink(regular, symlink); err == nil {
		if err := copyPath(symlink, filepath.Join(parent, "link-copy")); err == nil {
			t.Fatal("symbolic link was copied")
		}
	}
}

func TestCopyRejectsExistingDestination(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source.txt")
	destination := filepath.Join(parent, "destination.txt")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyPath(source, destination); err == nil {
		t.Fatal("copy replaced an existing destination")
	}
	content, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "existing" {
		t.Fatalf("existing destination changed to %q", content)
	}
}

func TestUploadAllowsSQLiteContent(t *testing.T) {
	target := filepath.Join(t.TempDir(), "innocent.txt")
	payload := append([]byte("SQLite format 3\x00"), make([]byte, 64)...)
	writer := &recordingFileWriter{}
	manager := newFileManager(writer)
	defer manager.close()

	manager.startUpload(fileRequest{Type: "file.upload.start", ID: "start", Path: target, Size: int64(len(payload))})
	uploadID := uploadIDFromResponse(t, writer.last(t))
	manager.uploadChunk(fileRequest{Type: "file.upload.chunk", ID: "chunk", UploadID: uploadID, Data: base64.StdEncoding.EncodeToString(payload)})
	if !responseOK(t, writer.last(t)) {
		t.Fatalf("upload chunk failed: %#v", writer.last(t))
	}
	manager.finishUpload(fileRequest{Type: "file.upload.finish", ID: "finish", UploadID: uploadID})
	if !responseOK(t, writer.last(t)) {
		t.Fatalf("SQLite content upload failed: %#v", writer.last(t))
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != string(payload) {
		t.Fatalf("uploaded SQLite content changed: content=%q err=%v", content, err)
	}
}

func TestRegularUploadCompletes(t *testing.T) {
	target := filepath.Join(t.TempDir(), "notes.txt")
	payload := []byte("regular remote file\n")
	writer := &recordingFileWriter{}
	manager := newFileManager(writer)
	defer manager.close()

	manager.startUpload(fileRequest{Type: "file.upload.start", ID: "start", Path: target, Size: int64(len(payload))})
	uploadID := uploadIDFromResponse(t, writer.last(t))
	manager.uploadChunk(fileRequest{Type: "file.upload.chunk", ID: "chunk", UploadID: uploadID, Data: base64.StdEncoding.EncodeToString(payload)})
	manager.finishUpload(fileRequest{Type: "file.upload.finish", ID: "finish", UploadID: uploadID})
	if !responseOK(t, writer.last(t)) {
		t.Fatalf("regular upload failed: %#v", writer.last(t))
	}
	actual, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(payload) {
		t.Fatalf("uploaded content = %q, want %q", actual, payload)
	}
}

func TestOverwriteUploadReplacesRegularFile(t *testing.T) {
	target := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := []byte("replacement")
	writer := &recordingFileWriter{}
	manager := newFileManager(writer)
	defer manager.close()

	manager.startUpload(fileRequest{Type: "file.upload.start", ID: "start", Path: target, Size: int64(len(payload)), Overwrite: true})
	uploadID := uploadIDFromResponse(t, writer.last(t))
	manager.uploadChunk(fileRequest{Type: "file.upload.chunk", ID: "chunk", UploadID: uploadID, Data: base64.StdEncoding.EncodeToString(payload)})
	manager.finishUpload(fileRequest{Type: "file.upload.finish", ID: "finish", UploadID: uploadID})
	if !responseOK(t, writer.last(t)) {
		t.Fatalf("overwrite upload failed: %#v", writer.last(t))
	}
	actual, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(payload) {
		t.Fatalf("overwritten content = %q, want %q", actual, payload)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".lite-backup-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("overwrite left backup files: %v", backups)
	}
}

func TestDownloadErrorUsesDownloadErrorFrame(t *testing.T) {
	writer := &recordingFileWriter{}
	manager := newFileManager(writer)
	defer manager.close()
	manager.download(fileRequest{Type: "file.download", ID: "download", Path: filepath.Join(t.TempDir(), "missing.txt")})
	response := writer.last(t)
	if response["type"] != "file.download.error" || response["id"] != "download" {
		t.Fatalf("unexpected download error response: %#v", response)
	}
}
