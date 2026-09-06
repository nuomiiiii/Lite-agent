package cmd

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/nuomiiiii/lite-agent/relocate"
)

const autoDiscoveryRetiredMessage = "auto-discovery registration is no longer available; open the original node in Lite and copy a normal -e -t install command"

type savedAgentIdentity struct {
	UUID  string
	Token string
}

type savedAgentTokenResult struct {
	UsedSavedFile bool
	UUID          string
}

func getSavedAgentTokenFilePath() string {
	execPath, err := os.Executable()
	if err != nil {
		log.Println("Failed to get executable path:", err)
		return "auto-discovery.json"
	}
	return filepath.Join(filepath.Dir(execPath), "auto-discovery.json")
}

func loadSavedAgentToken(path string) (*savedAgentIdentity, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read saved node token")
	}
	identity, err := relocate.ParseSavedAgentIdentity(data)
	if err != nil {
		return nil, err
	}
	return &savedAgentIdentity{UUID: identity.UUID, Token: identity.Token}, nil
}

func hasExplicitAgentToken() bool {
	return strings.TrimSpace(flags.Token) != ""
}

func hasLegacyAutoDiscoveryMarker() bool {
	return strings.TrimSpace(flags.AutoDiscoveryKey) != ""
}

func errAutoDiscoveryIdentity(reason string) error {
	return fmt.Errorf("%s (%s)", autoDiscoveryRetiredMessage, reason)
}

// applySavedAgentToken loads a previously issued node token from
// auto-discovery.json when a legacy auto-discovery launch marker is present
// and no explicit token was given. It never registers a new client.
func applySavedAgentToken() (savedAgentTokenResult, error) {
	return applySavedAgentTokenFrom(getSavedAgentTokenFilePath())
}

func applySavedAgentTokenFrom(path string) (savedAgentTokenResult, error) {
	if hasExplicitAgentToken() {
		return savedAgentTokenResult{}, nil
	}
	if !hasLegacyAutoDiscoveryMarker() {
		return savedAgentTokenResult{}, nil
	}

	config, err := loadSavedAgentToken(path)
	if err != nil {
		return savedAgentTokenResult{}, errAutoDiscoveryIdentity(err.Error())
	}
	if config == nil {
		return savedAgentTokenResult{}, errAutoDiscoveryIdentity("auto-discovery.json is missing")
	}
	flags.Token = config.Token
	log.Printf("using historical node identity for UUID: %s", config.UUID)
	if relocate.LooksLikeContainer() {
		log.Println("historical container keeps using auto-discovery.json; recreate with -e -t to match a normal install")
	}
	return savedAgentTokenResult{UsedSavedFile: true, UUID: config.UUID}, nil
}
