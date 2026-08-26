package relocate

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type spec struct {
	Name             string
	Binary           string
	Args             []string
	WorkDir          string
	Environment      []string
	EnvironmentFiles []string
	PlistLabel       string
}

var (
	lookupExecutable = os.Executable
	lookupStat       = func(path string) error {
		_, err := os.Stat(path)
		return err
	}
	detectPlanFn = detectPlan
)

type controller interface {
	DetectService(executable string) (string, bool)
	LegacyServiceExists(name string) bool
	Collect(name string) (spec, error)
	Install(next spec) error
	Start(name string) error
	Running(name string) bool
	PreventRestart(name string) error
	DisableNoStop(name string) error
	StopDisableRemove(name string) error
}

func RelocateIfNeeded() (bool, error) {
	if os.Getenv("LITE_AGENT_SKIP_RELOCATE") == "1" {
		return false, nil
	}
	ctrl := defaultController()
	relocated, err := doRelocate(runtime.GOOS, os.Args, os.Environ(), ctrl)
	if err != nil {
		return false, err
	}
	if leftoverErr := retireLeftoverLegacy(runtime.GOOS, ctrl); leftoverErr != nil {
		log.Println("failed to retire leftover komari-agent:", leftoverErr)
	}
	return relocated, nil
}

func doRelocate(goos string, args, environ []string, ctrl controller) (bool, error) {
	if ctrl == nil {
		return false, nil
	}
	if isContainer(lookupStat) {
		log.Println("container agent detected; skip layout relocation")
		return false, nil
	}

	executable, err := lookupExecutable()
	if err != nil {
		return false, fmt.Errorf("resolve executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}

	planned, ok := detectPlanFn(goos, executable, currentHome(), currentProgramFiles())
	if !ok {
		return false, nil
	}

	serviceName, found := ctrl.DetectService(executable)
	if found && serviceName != planned.From.Service {
		log.Printf("custom service name %s; skip default layout relocation", serviceName)
		return false, nil
	}
	if !found && !ctrl.LegacyServiceExists(planned.From.Service) {
		return false, nil
	}

	newBinary := filepath.Join(planned.To.Dir, planned.To.BinaryName)
	if ctrl.Running(planned.To.Service) {
		log.Printf("new service %s already running; handing off from %s", planned.To.Service, planned.From.Service)
		if err := os.MkdirAll(planned.To.Dir, 0o755); err != nil {
			return false, fmt.Errorf("create %s: %w", planned.To.Dir, err)
		}
		collected, _ := ctrl.Collect(planned.From.Service)
		if err := copySidecars(planned.From.Dir, planned.To.Dir, goos, sidecarExtras(args, collected)); err != nil {
			return false, err
		}
		_ = ctrl.PreventRestart(planned.From.Service)
		if err := ctrl.DisableNoStop(planned.From.Service); err != nil {
			log.Printf("disable %s: %v", planned.From.Service, err)
		}
		return true, nil
	}

	if err := os.MkdirAll(planned.To.Dir, 0o755); err != nil {
		return false, fmt.Errorf("create %s: %w", planned.To.Dir, err)
	}
	if err := copyFile(executable, newBinary); err != nil {
		return false, fmt.Errorf("copy binary: %w", err)
	}
	if goos != "windows" {
		_ = os.Chmod(newBinary, 0o755)
	}
	collected, err := ctrl.Collect(planned.From.Service)
	if err != nil {
		collected = spec{}
	}
	if err := copySidecars(planned.From.Dir, planned.To.Dir, goos, sidecarExtras(args, collected)); err != nil {
		return false, err
	}

	launchArgs := args
	if len(collected.Args) > 0 {
		launchArgs = append([]string{args[0]}, collected.Args...)
	}
	env := collected.Environment
	if len(env) == 0 {
		env = processAgentEnv(environ)
	}

	next := spec{
		Name:             planned.To.Service,
		Binary:           newBinary,
		Args:             rewriteLaunchArgs(launchArgs, planned.From.Dir, planned.To.Dir, newBinary, goos)[1:],
		WorkDir:          planned.To.Dir,
		Environment:      rewriteEnv(env, planned.From.Dir, planned.To.Dir, goos),
		EnvironmentFiles: rewriteEnvFiles(collected.EnvironmentFiles, planned.From.Dir, planned.To.Dir, goos),
		PlistLabel:       planned.To.PlistLabel,
	}
	if err := ctrl.Install(next); err != nil {
		return false, fmt.Errorf("install %s: %w", next.Name, err)
	}
	if err := ctrl.Start(next.Name); err != nil {
		return false, fmt.Errorf("start %s: %w", next.Name, err)
	}
	if !waitRunning(ctrl, next.Name, 15*time.Second) {
		return false, fmt.Errorf("new service %s did not become running", next.Name)
	}
	if err := ctrl.PreventRestart(planned.From.Service); err != nil {
		log.Printf("disable restart on %s: %v", planned.From.Service, err)
	}
	if err := ctrl.DisableNoStop(planned.From.Service); err != nil {
		return false, fmt.Errorf("disable %s: %w", planned.From.Service, err)
	}
	log.Printf("relocated agent from %s to %s (%s); old process will exit so leftover cleanup can remove %s", planned.From.Dir, planned.To.Dir, newBinary, planned.From.Service)
	return true, nil
}

func sidecarExtras(args []string, collected spec) []string {
	extras := configPathsFromArgs(args)
	extras = append(extras, collected.EnvironmentFiles...)
	return extras
}

func retireLeftoverLegacy(goos string, ctrl controller) error {
	if ctrl == nil || isContainer(lookupStat) {
		return nil
	}
	executable, err := lookupExecutable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	if _, onOldPath := detectPlanFn(goos, executable, currentHome(), currentProgramFiles()); onOldPath {
		return nil
	}
	serviceName, found := ctrl.DetectService(executable)
	if found && serviceName != newServiceName {
		return nil
	}
	if !found && !onModernLayout(goos, executable, currentHome(), currentProgramFiles()) {
		return nil
	}
	if !ctrl.LegacyServiceExists(legacyServiceName) {
		return nil
	}
	log.Printf("retiring leftover %s now that Lite-agent is running", legacyServiceName)
	_ = ctrl.PreventRestart(legacyServiceName)
	return ctrl.StopDisableRemove(legacyServiceName)
}

func rewriteEnvFiles(files []string, oldDir, newDir, goos string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		out = append(out, rewritePath(file, oldDir, newDir, goos))
	}
	return out
}

func waitRunning(ctrl controller, name string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ctrl.Running(name) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return ctrl.Running(name)
}
