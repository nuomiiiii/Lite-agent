//go:build !linux && !freebsd && !darwin && !windows

package relocate

func defaultController() controller { return noopController{} }

type noopController struct{}

func (noopController) DetectService(string) (string, bool) { return "", false }
func (noopController) LegacyServiceExists(string) bool     { return false }
func (noopController) Collect(string) (spec, error)        { return spec{}, nil }
func (noopController) Install(spec) error                  { return nil }
func (noopController) Start(string) error                  { return nil }
func (noopController) Running(string) bool                 { return false }
func (noopController) PreventRestart(string) error         { return nil }
func (noopController) DisableNoStop(string) error          { return nil }
func (noopController) StopDisableRemove(string) error      { return nil }
