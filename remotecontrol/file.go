package remotecontrol

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var renameFile = os.Rename

func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	destReady, err := replaceFile(tmpName, path)
	if err != nil {
		if !destReady {
			cleanup = false
		}
		return err
	}
	cleanup = false
	return os.Chmod(path, perm)
}

func replaceFile(tmpName, path string) (destReady bool, err error) {
	err = renameFile(tmpName, path)
	if err == nil {
		return true, nil
	}
	firstErr := err
	info, statErr := os.Lstat(path)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return false, firstErr
		}
		return false, errors.Join(firstErr, statErr)
	}
	if info.IsDir() {
		return true, firstErr
	}
	backup := filepath.Join(filepath.Dir(path), fmt.Sprintf(".%s-%d.bak", filepath.Base(path), time.Now().UnixNano()))
	if err := renameFile(path, backup); err != nil {
		return true, errors.Join(firstErr, err)
	}
	if err := renameFile(tmpName, path); err != nil {
		if restoreErr := renameFile(backup, path); restoreErr != nil {
			return false, errors.Join(firstErr, err, restoreErr)
		}
		return true, errors.Join(firstErr, err)
	}
	_ = os.Remove(backup)
	return true, nil
}
