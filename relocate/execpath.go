package relocate

import (
	"path/filepath"
	"runtime"
	"strings"
)

func officialServiceNames() []string {
	return []string{legacyServiceName, newServiceName}
}

func normalizeExecPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"'`)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

func pathsReferToSameExecutable(a, b string) bool {
	left := normalizeExecPath(a)
	right := normalizeExecPath(b)
	if left == "" || right == "" {
		return false
	}
	return left == right
}

func firstCommandToken(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if command[0] == '"' {
		end := strings.Index(command[1:], `"`)
		if end >= 0 {
			return command[1 : 1+end]
		}
	}
	if i := strings.IndexAny(command, " \t"); i > 0 {
		return strings.Trim(command[:i], `"'`)
	}
	return strings.Trim(command, `"'`)
}

func looksLikeNssm(binaryPath string) bool {
	base := strings.ToLower(filepath.Base(firstCommandToken(binaryPath)))
	return base == "nssm.exe" || base == "nssm"
}

func binaryPathRunsExecutable(binaryPath, executable string) bool {
	if looksLikeNssm(binaryPath) {
		return false
	}
	return pathsReferToSameExecutable(firstCommandToken(binaryPath), executable)
}
