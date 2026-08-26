//go:build darwin

package relocate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func defaultController() controller { return darwinController{} }

type darwinController struct{}

func (darwinController) DetectService(_ string) (string, bool) {
	for _, label := range []string{"com.komari.komari-agent", "com.lite.lite-agent"} {
		if fileExists("/Library/LaunchDaemons/"+label+".plist") ||
			fileExists(filepath.Join(os.Getenv("HOME"), "Library/LaunchAgents", label+".plist")) {
			if label == "com.komari.komari-agent" {
				return legacyServiceName, true
			}
			return newServiceName, true
		}
	}
	return "", false
}

func (darwinController) LegacyServiceExists(name string) bool {
	label := legacyPlistPrefix + name
	return fileExists("/Library/LaunchDaemons/"+label+".plist") ||
		fileExists(filepath.Join(os.Getenv("HOME"), "Library/LaunchAgents", label+".plist"))
}

func (darwinController) Collect(string) (spec, error) { return spec{}, nil }

func (c darwinController) Install(next spec) error {
	label := next.PlistLabel
	if label == "" {
		label = newPlistPrefix + next.Name
	}
	plistPath, system := plistPathFor(next.WorkDir, label)
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	body, err := launchdPlist(label, next, system)
	if err != nil {
		return err
	}
	return os.WriteFile(plistPath, body, 0o644)
}

func (darwinController) Start(name string) error {
	label := newPlistPrefix + name
	systemPlist := "/Library/LaunchDaemons/" + label + ".plist"
	userPlist := filepath.Join(os.Getenv("HOME"), "Library/LaunchAgents", label+".plist")
	if fileExists(systemPlist) {
		return run("launchctl", "bootstrap", "system", systemPlist)
	}
	if fileExists(userPlist) {
		return run("launchctl", "bootstrap", "gui/"+strconv.Itoa(os.Getuid()), userPlist)
	}
	return os.ErrNotExist
}

func (darwinController) Running(name string) bool {
	label := newPlistPrefix + name
	return exec.Command("launchctl", "print", "system/"+label).Run() == nil ||
		exec.Command("launchctl", "print", "gui/"+strconv.Itoa(os.Getuid())+"/"+label).Run() == nil
}

func (darwinController) PreventRestart(name string) error {
	for _, prefix := range []string{legacyPlistPrefix, newPlistPrefix} {
		label := prefix + name
		for _, path := range []string{
			"/Library/LaunchDaemons/" + label + ".plist",
			filepath.Join(os.Getenv("HOME"), "Library/LaunchAgents", label+".plist"),
		} {
			body, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			patched := strings.Replace(string(body), "<key>KeepAlive</key>\n    <true/>", "<key>KeepAlive</key>\n    <false/>", 1)
			if patched != string(body) {
				_ = os.WriteFile(path, []byte(patched), 0o644)
			}
		}
	}
	return nil
}

func (darwinController) DisableNoStop(name string) error {
	return darwinController{}.PreventRestart(name)
}

func (darwinController) StopDisableRemove(name string) error {
	for _, prefix := range []string{legacyPlistPrefix, newPlistPrefix} {
		label := prefix + name
		systemPlist := "/Library/LaunchDaemons/" + label + ".plist"
		userPlist := filepath.Join(os.Getenv("HOME"), "Library/LaunchAgents", label+".plist")
		_ = run("launchctl", "bootout", "system", systemPlist)
		_ = run("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid()), userPlist)
		_ = os.Remove(systemPlist)
		_ = os.Remove(userPlist)
	}
	return nil
}

func plistPathFor(workDir, label string) (string, bool) {
	if workDir != "" && (len(workDir) >= 7 && workDir[:7] == "/Users/") {
		return filepath.Join(os.Getenv("HOME"), "Library/LaunchAgents", label+".plist"), false
	}
	if os.Geteuid() != 0 {
		return filepath.Join(os.Getenv("HOME"), "Library/LaunchAgents", label+".plist"), false
	}
	return "/Library/LaunchDaemons/" + label + ".plist", true
}

func launchdPlist(label string, next spec, system bool) ([]byte, error) {
	user := "root"
	logDir := "/var/log"
	if !system {
		user = os.Getenv("USER")
		logDir = filepath.Join(os.Getenv("HOME"), "Library/Logs")
	}
	args := append([]string{next.Binary}, next.Args...)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
`, xmlEscape(label))
	for _, arg := range args {
		body += "        <string>" + xmlEscape(arg) + "</string>\n"
	}
	body += fmt.Sprintf(`    </array>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>UserName</key>
    <string>%s</string>
    <key>StandardOutPath</key>
    <string>%s/%s.log</string>
    <key>StandardErrorPath</key>
    <string>%s/%s.log</string>
</dict>
</plist>
`, xmlEscape(next.WorkDir), xmlEscape(user), xmlEscape(logDir), xmlEscape(next.Name), xmlEscape(logDir), xmlEscape(next.Name))
	return []byte(body), nil
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return replacer.Replace(value)
}
