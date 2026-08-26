package relocate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	legacyServiceName = "komari-agent"
	newServiceName    = "lite-agent"
	legacyPlistPrefix = "com.komari."
	newPlistPrefix    = "com.lite."
)

var sidecarNames = []string{
	"auto-discovery.json",
	"net_static.json",
	"net_static.json.bak",
	"nssm.exe",
}

type layout struct {
	Dir        string
	BinaryName string
	Service    string
	PlistLabel string
}

type plan struct {
	From layout
	To   layout
}

func defaultLayouts(goos, home, programFiles string) (legacy, modern []layout) {
	switch goos {
	case "windows":
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		legacy = []layout{{
			Dir:        filepath.Join(programFiles, "Komari"),
			BinaryName: "komari-agent.exe",
			Service:    legacyServiceName,
		}}
		modern = []layout{{
			Dir:        filepath.Join(programFiles, "Lite"),
			BinaryName: "Lite-agent.exe",
			Service:    newServiceName,
		}}
	case "darwin":
		legacy = []layout{
			{Dir: "/usr/local/komari", BinaryName: "agent", Service: legacyServiceName, PlistLabel: legacyPlistPrefix + legacyServiceName},
		}
		modern = []layout{
			{Dir: "/usr/local/lite-agent", BinaryName: "Lite-agent", Service: newServiceName, PlistLabel: newPlistPrefix + newServiceName},
		}
		if home != "" {
			legacy = append(legacy, layout{Dir: filepath.Join(home, ".komari"), BinaryName: "agent", Service: legacyServiceName, PlistLabel: legacyPlistPrefix + legacyServiceName})
			modern = append(modern, layout{Dir: filepath.Join(home, ".lite-agent"), BinaryName: "Lite-agent", Service: newServiceName, PlistLabel: newPlistPrefix + newServiceName})
		}
	default:
		legacy = []layout{{Dir: "/opt/komari", BinaryName: "agent", Service: legacyServiceName}}
		modern = []layout{{Dir: "/opt/lite-agent", BinaryName: "Lite-agent", Service: newServiceName}}
	}
	return legacy, modern
}

func detectPlan(goos, executable, home, programFiles string) (plan, bool) {
	execDir := filepath.Clean(filepath.Dir(executable))
	legacy, modern := defaultLayouts(goos, home, programFiles)
	for i, from := range legacy {
		if !sameDir(execDir, from.Dir, goos) {
			continue
		}
		to := modern[0]
		if i < len(modern) {
			to = modern[i]
		}
		if sameDir(execDir, to.Dir, goos) {
			return plan{}, false
		}
		from.Dir = execDir
		return plan{From: from, To: to}, true
	}
	return plan{}, false
}

func onModernLayout(goos, executable, home, programFiles string) bool {
	execDir := filepath.Clean(filepath.Dir(executable))
	_, modern := defaultLayouts(goos, home, programFiles)
	for _, to := range modern {
		if sameDir(execDir, to.Dir, goos) {
			return true
		}
	}
	return false
}

func sameDir(a, b, goos string) bool {
	left := filepath.Clean(a)
	right := filepath.Clean(b)
	if goos == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func inDir(path, dir, goos string) bool {
	path = filepath.Clean(path)
	dir = filepath.Clean(dir)
	if goos == "windows" {
		path = strings.ToLower(path)
		dir = strings.ToLower(dir)
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func rewritePath(path, oldDir, newDir, goos string) string {
	if path == "" || !inDir(path, oldDir, goos) {
		return path
	}
	rel, err := filepath.Rel(filepath.Clean(oldDir), filepath.Clean(path))
	if err != nil {
		return path
	}
	return filepath.Join(newDir, rel)
}

func rewriteLaunchArgs(args []string, oldDir, newDir, newBinary, goos string) []string {
	if len(args) == 0 {
		return []string{newBinary}
	}
	out := make([]string, len(args))
	copy(out, args)
	out[0] = newBinary
	for i := 1; i < len(out); i++ {
		arg := out[i]
		switch {
		case arg == "--config" || arg == "-config":
			if i+1 < len(out) {
				out[i+1] = rewritePath(out[i+1], oldDir, newDir, goos)
			}
		case strings.HasPrefix(arg, "--config="):
			out[i] = "--config=" + rewritePath(strings.TrimPrefix(arg, "--config="), oldDir, newDir, goos)
		}
	}
	return out
}

func rewriteEnv(env []string, oldDir, newDir, goos string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			out = append(out, item)
			continue
		}
		out = append(out, key+"="+rewritePath(value, oldDir, newDir, goos))
	}
	return out
}

func processAgentEnv(environ []string) []string {
	var out []string
	for _, item := range environ {
		if strings.HasPrefix(item, "AGENT_") || strings.HasPrefix(item, "HOST_PROC=") {
			out = append(out, item)
		}
	}
	return out
}

func isContainer(stat func(string) error) bool {
	for _, marker := range []string{"/.lite-agent-container", "/.komari-agent-container"} {
		if err := stat(marker); err == nil {
			return true
		}
	}
	return false
}

func currentHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func currentProgramFiles() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	if value := strings.TrimSpace(os.Getenv("ProgramFiles")); value != "" {
		return value
	}
	return `C:\Program Files`
}
