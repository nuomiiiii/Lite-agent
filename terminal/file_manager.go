package terminal

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxFileTransferSize = int64(2 << 30)
	maxFileChunkSize    = 384 << 10
	downloadChunkSize   = 256 << 10
)

type fileRequest struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	Path        string `json:"path,omitempty"`
	Destination string `json:"destination,omitempty"`
	UploadID    string `json:"upload_id,omitempty"`
	Data        string `json:"data,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Overwrite   bool   `json:"overwrite,omitempty"`
	Recursive   bool   `json:"recursive,omitempty"`
}

type fileEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Mode       string `json:"mode"`
	ModifiedAt string `json:"modified_at"`
	Directory  bool   `json:"directory"`
	Symlink    bool   `json:"symlink"`
	Hidden     bool   `json:"hidden"`
	Protected  bool   `json:"protected"`
}

type uploadState struct {
	mu        sync.Mutex
	file      *os.File
	tempPath  string
	target    string
	expected  int64
	received  int64
	overwrite bool
	hash      hash.Hash
}

type fileResponseWriter interface {
	writeJSON(value any) error
}

type fileManager struct {
	writer  fileResponseWriter
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	uploads map[string]*uploadState
}

func newFileManager(writer fileResponseWriter) *fileManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &fileManager{writer: writer, ctx: ctx, cancel: cancel, uploads: make(map[string]*uploadState)}
}

func (manager *fileManager) close() {
	manager.cancel()
	manager.mu.Lock()
	uploads := manager.uploads
	manager.uploads = make(map[string]*uploadState)
	manager.mu.Unlock()
	for _, upload := range uploads {
		upload.mu.Lock()
		_ = upload.file.Close()
		_ = os.Remove(upload.tempPath)
		upload.mu.Unlock()
	}
}

func isFileMessage(messageType string) bool {
	return strings.HasPrefix(messageType, "file.")
}

func (manager *fileManager) handle(payload []byte) {
	var request fileRequest
	if err := json.Unmarshal(payload, &request); err != nil || request.ID == "" {
		manager.respond(request, nil, errors.New("invalid file request"))
		return
	}
	switch request.Type {
	case "file.list":
		manager.list(request)
	case "file.mkdir":
		manager.mkdir(request)
	case "file.create":
		manager.create(request)
	case "file.rename":
		manager.rename(request)
	case "file.copy":
		manager.copy(request)
	case "file.delete":
		manager.remove(request)
	case "file.upload.start":
		manager.startUpload(request)
	case "file.upload.chunk":
		manager.uploadChunk(request)
	case "file.upload.finish":
		manager.finishUpload(request)
	case "file.download":
		go manager.download(request)
	default:
		manager.respond(request, nil, errors.New("unsupported file operation"))
	}
}

func (manager *fileManager) respond(request fileRequest, data any, err error) {
	response := map[string]any{
		"type":      "file.response",
		"id":        request.ID,
		"operation": request.Type,
		"ok":        err == nil,
	}
	if err != nil {
		response["error"] = err.Error()
	} else if data != nil {
		response["data"] = data
	}
	_ = manager.writer.writeJSON(response)
}

func userHomeDirectory() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filesystemRoots()[0]
	}
	return home
}

func normalizeFilePath(value string) (string, error) {
	if strings.ContainsRune(value, '\x00') {
		return "", errors.New("invalid path")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return filesystemRoots()[0], nil
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(filesystemRoots()[0], value)
	}
	return filepath.Clean(value), nil
}

func parentFilePath(path string) string {
	cleaned := filepath.Clean(path)
	parent := filepath.Dir(cleaned)
	if parent == cleaned {
		return ""
	}
	return parent
}

func isFilesystemRoot(path string) bool {
	cleaned := filepath.Clean(path)
	if filepath.Dir(cleaned) == cleaned {
		return true
	}
	for _, root := range filesystemRoots() {
		if strings.EqualFold(cleaned, filepath.Clean(root)) {
			return true
		}
	}
	return false
}

func (manager *fileManager) list(request fileRequest) {
	path, err := normalizeFilePath(request.Path)
	if err != nil {
		manager.respond(request, nil, err)
		return
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		manager.respond(request, nil, err)
		return
	}
	result := make([]fileEntry, 0, len(entries))
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		fullPath := filepath.Join(path, entry.Name())
		result = append(result, fileEntry{
			Name:       entry.Name(),
			Path:       fullPath,
			Size:       info.Size(),
			Mode:       info.Mode().String(),
			ModifiedAt: info.ModTime().UTC().Format(time.RFC3339),
			Directory:  entry.IsDir(),
			Symlink:    entry.Type()&os.ModeSymlink != 0,
			Hidden:     strings.HasPrefix(entry.Name(), "."),
			Protected:  false,
		})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Directory != result[right].Directory {
			return result[left].Directory
		}
		return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
	})
	manager.respond(request, map[string]any{
		"path":    path,
		"parent":  parentFilePath(path),
		"roots":   filesystemRoots(),
		"entries": result,
	}, nil)
}

func (manager *fileManager) mkdir(request fileRequest) {
	path, err := normalizeFilePath(request.Path)
	if err == nil {
		err = os.Mkdir(path, 0o755)
	}
	manager.respond(request, nil, err)
}

func (manager *fileManager) create(request fileRequest) {
	path, err := normalizeFilePath(request.Path)
	if err == nil {
		var file *os.File
		file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if file != nil {
			err = errors.Join(err, file.Close())
		}
	}
	manager.respond(request, nil, err)
}

func (manager *fileManager) rename(request fileRequest) {
	source, err := normalizeFilePath(request.Path)
	if err != nil {
		manager.respond(request, nil, err)
		return
	}
	if isFilesystemRoot(source) {
		manager.respond(request, nil, errors.New("filesystem roots cannot be renamed"))
		return
	}
	destination, err := normalizeFilePath(request.Destination)
	if err == nil {
		if _, statErr := os.Lstat(destination); statErr == nil {
			err = errors.New("destination already exists")
		} else if !errors.Is(statErr, os.ErrNotExist) {
			err = statErr
		}
	}
	if err == nil {
		err = os.Rename(source, destination)
	}
	manager.respond(request, nil, err)
}

func (manager *fileManager) copy(request fileRequest) {
	source, err := normalizeFilePath(request.Path)
	if err != nil {
		manager.respond(request, nil, err)
		return
	}
	destination, err := normalizeFilePath(request.Destination)
	if err == nil {
		err = copyPath(source, destination)
	}
	manager.respond(request, nil, err)
}

func copyPath(source, destination string) error {
	if isFilesystemRoot(source) || isFilesystemRoot(destination) {
		return errors.New("filesystem roots cannot be copied")
	}
	sourceInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("symbolic links cannot be copied")
	}
	if _, err := os.Lstat(destination); err == nil {
		return errors.New("destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if hasSymlink, err := pathHasSymlink(source); err != nil {
		return err
	} else if hasSymlink {
		return errors.New("paths containing symbolic links cannot be copied")
	}
	if hasSymlink, err := pathHasSymlink(filepath.Dir(destination)); err != nil {
		return err
	} else if hasSymlink {
		return errors.New("copy destinations cannot use symbolic links")
	}
	resolvedSource, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	resolvedDestination, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if sourceInfo.IsDir() {
		if pathContains(resolvedSource, resolvedDestination) {
			return errors.New("a directory cannot be copied into itself")
		}
		if err := validateCopyDirectory(source); err != nil {
			return err
		}
		return copyDirectory(source, destination)
	}
	if !sourceInfo.Mode().IsRegular() {
		return errors.New("only regular files and directories can be copied")
	}
	return copyRegularFile(source, destination, sourceInfo.Mode().Perm())
}

func pathHasSymlink(path string) (bool, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	for {
		info, err := os.Lstat(current)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		} else if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
		current = parent
	}
}

func pathContains(parent, child string) bool {
	if runtime.GOOS == "windows" {
		parent = strings.ToLower(parent)
		child = strings.ToLower(child)
	}
	relative, err := filepath.Rel(parent, child)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func validateCopyDirectory(source string) error {
	return filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("directories containing symbolic links cannot be copied")
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return errors.New("directories containing special files cannot be copied")
		}
		return nil
	})
}

func copyDirectory(source, destination string) (err error) {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return err
	}
	if err := os.Mkdir(destination, sourceInfo.Mode().Perm()); err != nil {
		return err
	}
	complete := false
	defer func() {
		if !complete {
			err = errors.Join(err, os.RemoveAll(destination))
		}
	}()
	err = filepath.WalkDir(source, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == source {
			return nil
		}
		relative, err := filepath.Rel(source, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Mkdir(target, info.Mode().Perm())
		}
		return copyRegularFile(current, target, info.Mode().Perm())
	})
	if err != nil {
		return err
	}
	complete = true
	return nil
}

func copyRegularFile(source, destination string, mode fs.FileMode) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		err = errors.Join(err, output.Close())
		if !complete {
			err = errors.Join(err, os.Remove(destination))
		}
	}()
	if _, err = io.Copy(output, input); err != nil {
		return err
	}
	if err = output.Sync(); err != nil {
		return err
	}
	complete = true
	return nil
}

func (manager *fileManager) remove(request fileRequest) {
	path, err := normalizeFilePath(request.Path)
	if err == nil && isFilesystemRoot(path) {
		err = errors.New("filesystem roots cannot be deleted")
	}
	if err == nil {
		if info, statErr := os.Lstat(path); statErr != nil {
			err = statErr
		} else if info.IsDir() && !request.Recursive {
			err = errors.New("directory deletion requires recursive confirmation")
		}
	}
	if err == nil {
		if request.Recursive {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
	}
	manager.respond(request, nil, err)
}

func newTransferID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func (manager *fileManager) startUpload(request fileRequest) {
	target, err := normalizeFilePath(request.Path)
	if err == nil {
		err = validateUploadTarget(target, request.Overwrite)
	}
	if err == nil && (request.Size < 0 || request.Size > maxFileTransferSize) {
		err = fmt.Errorf("file exceeds the %d byte transfer limit", maxFileTransferSize)
	}
	var tempFile *os.File
	if err == nil {
		tempFile, err = os.CreateTemp(filepath.Dir(target), ".lite-upload-*")
	}
	if err != nil {
		manager.respond(request, nil, err)
		return
	}
	_ = tempFile.Chmod(0o600)
	uploadID, err := newTransferID()
	if err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		manager.respond(request, nil, err)
		return
	}
	manager.mu.Lock()
	manager.uploads[uploadID] = &uploadState{
		file: tempFile, tempPath: tempFile.Name(), target: target,
		expected: request.Size, overwrite: request.Overwrite, hash: sha256.New(),
	}
	manager.mu.Unlock()
	manager.respond(request, map[string]any{"upload_id": uploadID}, nil)
}

func (manager *fileManager) uploadChunk(request fileRequest) {
	manager.mu.Lock()
	upload := manager.uploads[request.UploadID]
	manager.mu.Unlock()
	if upload == nil {
		manager.respond(request, nil, errors.New("upload session not found"))
		return
	}
	data, err := base64.StdEncoding.DecodeString(request.Data)
	if err == nil && len(data) > maxFileChunkSize {
		err = errors.New("upload chunk is too large")
	}
	upload.mu.Lock()
	if err == nil && upload.received+int64(len(data)) > upload.expected {
		err = errors.New("upload exceeds declared size")
	}
	if err == nil {
		_, err = upload.file.Write(data)
	}
	if err == nil {
		upload.received += int64(len(data))
		_, _ = upload.hash.Write(data)
	}
	received := upload.received
	upload.mu.Unlock()
	manager.respond(request, map[string]any{"received": received}, err)
}

func (manager *fileManager) finishUpload(request fileRequest) {
	manager.mu.Lock()
	upload := manager.uploads[request.UploadID]
	delete(manager.uploads, request.UploadID)
	manager.mu.Unlock()
	if upload == nil {
		manager.respond(request, nil, errors.New("upload session not found"))
		return
	}
	upload.mu.Lock()
	defer upload.mu.Unlock()
	cleanup := true
	defer func() {
		if cleanup {
			_ = upload.file.Close()
			_ = os.Remove(upload.tempPath)
		}
	}()
	if upload.received != upload.expected {
		manager.respond(request, nil, errors.New("uploaded size does not match declared size"))
		return
	}
	actualHash := hex.EncodeToString(upload.hash.Sum(nil))
	if request.SHA256 != "" && !strings.EqualFold(request.SHA256, actualHash) {
		manager.respond(request, nil, errors.New("uploaded file checksum mismatch"))
		return
	}
	if err := upload.file.Sync(); err != nil {
		manager.respond(request, nil, err)
		return
	}
	if err := upload.file.Close(); err != nil {
		manager.respond(request, nil, err)
		return
	}
	if err := validateUploadTarget(upload.target, upload.overwrite); err != nil {
		manager.respond(request, nil, err)
		return
	}
	if err := replaceUploadedFile(upload.tempPath, upload.target, upload.overwrite); err != nil {
		manager.respond(request, nil, err)
		return
	}
	cleanup = false
	manager.respond(request, map[string]any{"sha256": actualHash, "size": upload.received}, nil)
}

func replaceUploadedFile(tempPath, target string, overwrite bool) error {
	if !overwrite || runtime.GOOS != "windows" {
		return os.Rename(tempPath, target)
	}
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return os.Rename(tempPath, target)
	} else if err != nil {
		return err
	}
	backupID, err := newTransferID()
	if err != nil {
		return err
	}
	backup := filepath.Join(filepath.Dir(target), ".lite-backup-"+backupID)
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(tempPath, target); err != nil {
		restoreErr := os.Rename(backup, target)
		return errors.Join(err, restoreErr)
	}
	_ = os.Remove(backup)
	return nil
}

func validateUploadTarget(target string, overwrite bool) error {
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("upload target must be a regular file")
	}
	if !overwrite {
		return errors.New("destination already exists")
	}
	return nil
}

func (manager *fileManager) download(request fileRequest) {
	path, err := normalizeFilePath(request.Path)
	var file *os.File
	var info os.FileInfo
	if err == nil {
		info, err = os.Lstat(path)
	}
	if err == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		err = errors.New("only regular files can be downloaded")
	}
	if err == nil && info.Size() > maxFileTransferSize {
		err = fmt.Errorf("file exceeds the %d byte transfer limit", maxFileTransferSize)
	}
	if err == nil {
		file, err = os.Open(path)
	}
	if err != nil {
		_ = manager.writer.writeJSON(map[string]any{
			"type": "file.download.error", "id": request.ID, "error": err.Error(),
		})
		return
	}
	defer file.Close()
	_ = manager.writer.writeJSON(map[string]any{
		"type": "file.download.begin", "id": request.ID,
		"name": filepath.Base(path), "size": info.Size(),
	})
	hasher := sha256.New()
	buffer := make([]byte, downloadChunkSize)
	sequence := 0
	for {
		select {
		case <-manager.ctx.Done():
			return
		default:
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			_, _ = hasher.Write(buffer[:count])
			if err := manager.writer.writeJSON(map[string]any{
				"type": "file.download.chunk", "id": request.ID, "sequence": sequence,
				"data": base64.StdEncoding.EncodeToString(buffer[:count]),
			}); err != nil {
				return
			}
			sequence++
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			_ = manager.writer.writeJSON(map[string]any{"type": "file.download.error", "id": request.ID, "error": readErr.Error()})
			return
		}
	}
	_ = manager.writer.writeJSON(map[string]any{
		"type": "file.download.end", "id": request.ID,
		"sha256": hex.EncodeToString(hasher.Sum(nil)),
	})
}
