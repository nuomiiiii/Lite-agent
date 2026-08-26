//go:build windows

package relocate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func defaultController() controller { return windowsController{} }

type windowsController struct{}

func (windowsController) DetectService(executable string) (string, bool) {
	m, err := mgr.Connect()
	if err != nil {
		return "", false
	}
	defer m.Disconnect()
	names, err := m.ListServices()
	if err != nil {
		return "", false
	}
	want := strings.ToLower(filepath.Clean(executable))
	for _, name := range names {
		s, err := m.OpenService(name)
		if err != nil {
			continue
		}
		cfg, err := s.Config()
		s.Close()
		if err != nil {
			continue
		}
		path := strings.ToLower(cfg.BinaryPathName)
		if strings.Contains(path, want) || strings.Contains(path, strings.ToLower(filepath.Base(executable))) {
			return name, true
		}
	}
	return "", false
}

func (windowsController) LegacyServiceExists(name string) bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return false
	}
	s.Close()
	return true
}

func (windowsController) Collect(name string) (spec, error) {
	next := spec{Name: name}
	nssm := nssmPath("")
	if dir, err := osExecutableDir(); err == nil {
		nssm = nssmPath(dir)
	}
	if out, err := exec.Command(nssm, "get", name, "AppDirectory").Output(); err == nil {
		next.WorkDir = decodeNssmOutput(out)
	}
	if out, err := exec.Command(nssm, "get", name, "AppParameters").Output(); err == nil {
		next.Args = strings.Fields(decodeNssmOutput(out))
	}
	if out, err := exec.Command(nssm, "get", name, "AppEnvironmentExtra").Output(); err == nil {
		for _, line := range strings.Split(decodeNssmOutput(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				next.Environment = append(next.Environment, line)
			}
		}
	}
	return next, nil
}

func (windowsController) Install(next spec) error {
	nssm := nssmPath(next.WorkDir)
	_ = exec.Command(nssm, "stop", next.Name).Run()
	_ = exec.Command(nssm, "remove", next.Name, "confirm").Run()
	args := append([]string{"install", next.Name, next.Binary}, next.Args...)
	if err := run(nssm, args...); err != nil {
		return err
	}
	_ = run(nssm, "set", next.Name, "DisplayName", "Lite Agent Service")
	_ = run(nssm, "set", next.Name, "Start", "SERVICE_AUTO_START")
	_ = run(nssm, "set", next.Name, "AppExit", "Default", "Restart")
	_ = run(nssm, "set", next.Name, "AppRestartDelay", "5000")
	_ = run(nssm, "set", next.Name, "AppDirectory", next.WorkDir)
	if len(next.Environment) > 0 {
		envArgs := append([]string{"set", next.Name, "AppEnvironmentExtra"}, next.Environment...)
		_ = run(nssm, envArgs...)
	}
	return nil
}

func (windowsController) Start(name string) error {
	return run(nssmPath(filepath.Join(currentProgramFiles(), "Lite")), "start", name)
}

func (windowsController) Running(name string) bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return false
	}
	defer s.Close()
	status, err := s.Query()
	if err != nil {
		return false
	}
	return status.State == svc.Running
}

func (windowsController) PreventRestart(name string) error {
	return run(windowsNssm(), "set", name, "AppExit", "Default", "Exit")
}

func (windowsController) DisableNoStop(name string) error {
	nssm := windowsNssm()
	_ = run(nssm, "set", name, "Start", "SERVICE_DISABLED")
	return nil
}

func (windowsController) StopDisableRemove(name string) error {
	nssm := windowsNssm()
	_ = exec.Command(nssm, "stop", name).Run()
	_ = exec.Command(nssm, "remove", name, "confirm").Run()
	return nil
}

func windowsNssm() string {
	dir := ""
	if found, err := osExecutableDir(); err == nil {
		dir = found
	}
	return nssmPath(dir)
}

func osExecutableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}
