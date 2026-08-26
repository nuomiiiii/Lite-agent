package relocate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func copyFile(src, dst string) error {
	if src == "" || dst == "" {
		return fmt.Errorf("missing copy path")
	}
	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func copySidecars(oldDir, newDir, goos string, extra []string) error {
	seen := map[string]struct{}{}
	var files []string
	for _, name := range sidecarNames {
		files = append(files, filepath.Join(oldDir, name))
	}
	files = append(files, extra...)
	for _, src := range files {
		if src == "" {
			continue
		}
		if _, ok := seen[src]; ok {
			continue
		}
		seen[src] = struct{}{}
		if _, err := os.Stat(src); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if !inDir(src, oldDir, goos) {
			continue
		}
		dst := rewritePath(src, oldDir, newDir, goos)
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", src, err)
		}
	}
	return nil
}

func configPathsFromArgs(args []string) []string {
	var files []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config" || arg == "-config":
			if i+1 < len(args) {
				files = append(files, args[i+1])
			}
		case strings.HasPrefix(arg, "--config="):
			files = append(files, strings.TrimPrefix(arg, "--config="))
		}
	}
	return files
}
