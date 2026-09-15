package terminal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMCPFileSessionKeepsUploadAcrossCalls(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "out.bin")
	leaseID := "ls_session_upload"
	t.Cleanup(func() { CloseMCPFileSession(leaseID) })

	startRaw, _ := json.Marshal(map[string]any{
		"type":      "file.upload.start",
		"id":        "start",
		"path":      target,
		"size":      4,
		"overwrite": true,
	})
	started, err := ExecuteMCPFileForLease(context.Background(), leaseID, startRaw)
	if err != nil {
		t.Fatal(err)
	}
	uploadID := fileResponseData(t, started)["upload_id"].(string)
	if uploadID == "" {
		t.Fatalf("missing upload_id: %#v", started)
	}

	chunkRaw, _ := json.Marshal(map[string]any{
		"type":      "file.upload.chunk",
		"id":        "chunk",
		"upload_id": uploadID,
		"data":      base64.StdEncoding.EncodeToString([]byte("abcd")),
	})
	if _, err := ExecuteMCPFileForLease(context.Background(), leaseID, chunkRaw); err != nil {
		t.Fatalf("chunk: %v", err)
	}

	finishRaw, _ := json.Marshal(map[string]any{
		"type":      "file.upload.finish",
		"id":        "finish",
		"upload_id": uploadID,
	})
	if _, err := ExecuteMCPFileForLease(context.Background(), leaseID, finishRaw); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abcd" {
		t.Fatalf("uploaded file = %q", got)
	}
}

func TestMCPFileDownloadHandleReadsChunks(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "src.bin")
	if err := os.WriteFile(target, []byte("hello-download"), 0o600); err != nil {
		t.Fatal(err)
	}
	leaseID := "ls_session_download"
	t.Cleanup(func() { CloseMCPFileSession(leaseID) })

	beginRaw, _ := json.Marshal(map[string]any{
		"type":        "file.download.begin",
		"id":          "begin",
		"path":        target,
		"download_id": "dl_test",
	})
	begun, err := ExecuteMCPFileForLease(context.Background(), leaseID, beginRaw)
	if err != nil {
		t.Fatal(err)
	}
	if fileResponseData(t, begun)["download_id"] != "dl_test" {
		t.Fatalf("begin = %#v", begun)
	}

	readRaw, _ := json.Marshal(map[string]any{
		"type":        "file.download.read",
		"id":          "read",
		"download_id": "dl_test",
		"offset":      0,
		"max_bytes":   5,
	})
	read, err := ExecuteMCPFileForLease(context.Background(), leaseID, readRaw)
	if err != nil {
		t.Fatal(err)
	}
	content, _ := base64.StdEncoding.DecodeString(fileResponseData(t, read)["content"].(string))
	if string(content) != "hello" {
		t.Fatalf("chunk = %q", content)
	}
}

func fileResponseData(t *testing.T, value any) map[string]any {
	t.Helper()
	message, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("response type = %T", value)
	}
	if okFlag, _ := message["ok"].(bool); !okFlag {
		t.Fatalf("file response failed: %#v", message)
	}
	data, _ := message["data"].(map[string]any)
	if data == nil {
		t.Fatalf("missing data: %#v", message)
	}
	return data
}

func TestMCPFileCancelledWhileWaitingDoesNotWrite(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "late-write.txt")
	leaseID := "ls_cancel_before_write"
	t.Cleanup(func() { CloseMCPFileSession(leaseID) })

	session := getMCPFileSession(leaseID)
	session.mu.Lock()

	payload, _ := json.Marshal(map[string]any{
		"type":     "file.write",
		"id":       "write",
		"path":     target,
		"content":  "should-not-land",
		"encoding": "utf-8",
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := ExecuteMCPFileForLease(ctx, leaseID, payload)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	session.mu.Unlock()
	if err := <-done; err == nil {
		t.Fatal("cancelled wait should return an error")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("cancelled write created %s", target)
	}
}
