//go:build linux || freebsd

package relocate

import (
	"os"
	"os/exec"
	"strings"
)

func defaultController() controller { return unixController{} }

type unixController struct{}

func (unixController) DetectService(_ string) (string, bool) {
	content, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", false
	}
	name := serviceFromCgroup(string(content))
	if name == "" {
		return "", false
	}
	return name, true
}

func (c unixController) LegacyServiceExists(name string) bool {
	return fileExists("/etc/systemd/system/"+name+".service") ||
		fileExists("/etc/init.d/"+name) ||
		fileExists("/etc/init/"+name+".conf")
}

func (unixController) Collect(name string) (spec, error) {
	next := spec{Name: name}
	if !commandExists("systemctl") {
		return next, nil
	}
	output, err := exec.Command("systemctl", "show", name, "--property=Environment", "--property=EnvironmentFiles", "--property=WorkingDirectory", "--no-pager").Output()
	if err != nil {
		return next, nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "WorkingDirectory":
			next.WorkDir = value
		case "Environment":
			if value != "" {
				next.Environment = append(next.Environment, strings.Fields(value)...)
			}
		case "EnvironmentFiles":
			if path, _, _ := strings.Cut(strings.Trim(value, "[]"), " "); path != "" && path != "/dev/null" {
				next.EnvironmentFiles = append(next.EnvironmentFiles, path)
			}
		}
	}
	return next, nil
}

func (unixController) Install(next spec) error {
	if commandExists("systemctl") {
		path := "/etc/systemd/system/" + next.Name + ".service"
		if err := os.WriteFile(path, []byte(systemdUnit(next)), 0o644); err != nil {
			return err
		}
		if err := run("systemctl", "daemon-reload"); err != nil {
			return err
		}
		return run("systemctl", "enable", next.Name+".service")
	}
	if commandExists("rc-service") {
		body := "#!/sbin/openrc-run\n\nname=\"Lite Agent Service\"\ncommand=\"" + next.Binary + "\"\ncommand_args=\"" + strings.Join(next.Args, " ") + "\"\ncommand_user=\"root\"\ndirectory=\"" + next.WorkDir + "\"\npidfile=\"/run/" + next.Name + ".pid\"\nretry=\"SIGTERM/30\"\nsupervisor=supervise-daemon\n"
		path := "/etc/init.d/" + next.Name
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			return err
		}
		return run("rc-update", "add", next.Name, "default")
	}
	if commandExists("uci") {
		body := "#!/bin/sh /etc/rc.common\n\nSTART=99\nSTOP=10\nUSE_PROCD=1\nPROG=\"" + next.Binary + "\"\nARGS=\"" + strings.Join(next.Args, " ") + "\"\n\nstart_service() {\n    procd_open_instance\n    procd_set_param command $PROG $ARGS\n    procd_set_param respawn\n    procd_set_param stdout 1\n    procd_set_param stderr 1\n    procd_set_param user root\n    procd_close_instance\n}\n\nstop_service() {\n    killall $(basename $PROG)\n}\n"
		path := "/etc/init.d/" + next.Name
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			return err
		}
		return run("/etc/init.d/"+next.Name, "enable")
	}
	if commandExists("initctl") {
		body := "description \"Lite Agent Service\"\nchdir " + next.WorkDir + "\nstart on filesystem or runlevel [2345]\nstop on runlevel [!2345]\nrespawn\nconsole none\nscript\n    exec " + next.Binary + " " + strings.Join(next.Args, " ") + "\nend script\n"
		if err := os.WriteFile("/etc/init/"+next.Name+".conf", []byte(body), 0o644); err != nil {
			return err
		}
		return run("initctl", "reload-configuration")
	}
	return os.ErrNotExist
}

func (unixController) Start(name string) error {
	if commandExists("systemctl") {
		return run("systemctl", "start", name+".service")
	}
	if commandExists("rc-service") {
		return run("rc-service", name, "start")
	}
	if fileExists("/etc/init.d/" + name) {
		return run("/etc/init.d/"+name, "start")
	}
	if commandExists("initctl") {
		return run("initctl", "start", name)
	}
	return os.ErrNotExist
}

func (unixController) Running(name string) bool {
	if commandExists("systemctl") {
		return exec.Command("systemctl", "is-active", "--quiet", name+".service").Run() == nil
	}
	if commandExists("rc-service") {
		return exec.Command("rc-service", name, "status").Run() == nil
	}
	if fileExists("/etc/init.d/" + name) {
		return exec.Command("/etc/init.d/"+name, "status").Run() == nil
	}
	return false
}

func (unixController) PreventRestart(name string) error {
	dropIn := "/etc/systemd/system/" + name + ".service.d"
	if commandExists("systemctl") && fileExists("/etc/systemd/system/"+name+".service") {
		if err := os.MkdirAll(dropIn, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dropIn+"/restart.conf", []byte("[Service]\nRestart=no\n"), 0o644); err != nil {
			return err
		}
		return run("systemctl", "daemon-reload")
	}
	return nil
}

func (unixController) DisableNoStop(name string) error {
	if commandExists("systemctl") {
		_ = run("systemctl", "disable", name+".service")
		return nil
	}
	if commandExists("rc-service") {
		_ = run("rc-update", "del", name, "default")
		return nil
	}
	if fileExists("/etc/init.d/" + name) {
		_ = run("/etc/init.d/"+name, "disable")
		return nil
	}
	return nil
}

func (unixController) StopDisableRemove(name string) error {
	if commandExists("systemctl") {
		_ = run("systemctl", "stop", name+".service")
		_ = run("systemctl", "disable", name+".service")
		_ = os.Remove("/etc/systemd/system/" + name + ".service")
		_ = os.RemoveAll("/etc/systemd/system/" + name + ".service.d")
		_ = run("systemctl", "daemon-reload")
		return nil
	}
	if commandExists("rc-service") {
		_ = run("rc-service", name, "stop")
		_ = run("rc-update", "del", name, "default")
		_ = os.Remove("/etc/init.d/" + name)
		return nil
	}
	if fileExists("/etc/init.d/" + name) {
		_ = run("/etc/init.d/"+name, "stop")
		_ = run("/etc/init.d/"+name, "disable")
		_ = os.Remove("/etc/init.d/" + name)
		return nil
	}
	if commandExists("initctl") {
		_ = run("initctl", "stop", name)
		_ = os.Remove("/etc/init/" + name + ".conf")
		return nil
	}
	return nil
}
