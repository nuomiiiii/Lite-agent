package relocate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const savedIdentityFileName = "auto-discovery.json"

type savedAgentIdentity struct {
	UUID  string `json:"uuid"`
	Token string `json:"token"`
}

func ParseSavedAgentIdentity(data []byte) (savedAgentIdentity, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return savedAgentIdentity{}, fmt.Errorf("saved node identity is empty")
	}
	var identity savedAgentIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return savedAgentIdentity{}, fmt.Errorf("saved node identity is not valid JSON")
	}
	identity.UUID = strings.TrimSpace(identity.UUID)
	identity.Token = strings.TrimSpace(identity.Token)
	if identity.UUID == "" || identity.Token == "" {
		return savedAgentIdentity{}, fmt.Errorf("saved node identity is missing uuid or token")
	}
	return identity, nil
}

func ValidateSavedIdentityFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("auto-discovery.json is missing")
		}
		return fmt.Errorf("failed to read saved node identity")
	}
	_, err = ParseSavedAgentIdentity(data)
	return err
}

func launchHasExplicitToken(args, env []string) bool {
	if strings.TrimSpace(namedFlagValue(args, "token")) != "" {
		return true
	}
	return strings.TrimSpace(envValue(env, "AGENT_TOKEN")) != ""
}

func launchHasLegacyAutoDiscovery(args, env []string) bool {
	if hasNamedFlag(args, "auto-discovery") {
		return true
	}
	return strings.TrimSpace(envValue(env, "AGENT_AUTO_DISCOVERY_KEY")) != ""
}

func requireSavedIdentityForLegacyLaunch(args, environ []string, collected spec, fromDir string) error {
	launchArgs := args
	if len(collected.Args) > 0 {
		prefix := "Lite-agent"
		if len(args) > 0 {
			prefix = args[0]
		}
		launchArgs = append([]string{prefix}, collected.Args...)
	}
	env := collected.Environment
	if len(env) == 0 {
		env = processAgentEnv(environ)
	}
	processEnv := processAgentEnv(environ)
	if launchHasExplicitToken(args, processEnv) || launchHasExplicitToken(launchArgs, env) {
		return nil
	}
	if !launchHasLegacyAutoDiscovery(args, processEnv) && !launchHasLegacyAutoDiscovery(launchArgs, env) {
		return nil
	}
	if err := ValidateSavedIdentityFile(filepath.Join(fromDir, savedIdentityFileName)); err != nil {
		return fmt.Errorf("auto-discovery registration is no longer available; open the original node in Lite and copy a normal -e -t install command (%v)", err)
	}
	return nil
}

func namedFlagValue(args []string, name string) string {
	long := "--" + name
	short := ""
	switch name {
	case "token":
		short = "-t"
	case "endpoint":
		short = "-e"
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if short != "" {
			if arg == short {
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					return args[i+1]
				}
				return ""
			}
			if strings.HasPrefix(arg, short+"=") {
				return strings.TrimPrefix(arg, short+"=")
			}
		}
		if arg == long {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(arg, long+"=") {
			return strings.TrimPrefix(arg, long+"=")
		}
	}
	return ""
}

func hasNamedFlag(args []string, name string) bool {
	long := "--" + name
	short := ""
	if name == "token" {
		short = "-t"
	}
	if name == "endpoint" {
		short = "-e"
	}
	for _, arg := range args {
		if short != "" && (arg == short || strings.HasPrefix(arg, short+"=")) {
			return true
		}
		if arg == long || strings.HasPrefix(arg, long+"=") {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) string {
	for _, item := range env {
		name, value, ok := strings.Cut(item, "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), key) {
			return value
		}
	}
	return ""
}
