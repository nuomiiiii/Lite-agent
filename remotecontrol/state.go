package remotecontrol

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const StateFileName = "remote-control.state"

type State struct {
	RemoteControlEnabled bool `json:"remote_control_enabled"`
}

func PathForExecutable(executable string) string {
	if executable == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(executable), StateFileName)
}

func Read(path string) (enabled bool, ok bool, err error) {
	if path == "" {
		return false, false, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, false, err
	}
	if _, present := raw["remote_control_enabled"]; !present {
		return false, false, nil
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return false, false, err
	}
	return state.RemoteControlEnabled, true, nil
}

func WriteAtomic(path string, enabled bool) error {
	payload, err := json.Marshal(State{RemoteControlEnabled: enabled})
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, append(payload, '\n'), 0o600)
}
