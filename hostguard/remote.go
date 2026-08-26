package hostguard

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

const cacheDuration = 15 * time.Second

var detectionCache struct {
	sync.Mutex
	checkedAt         time.Time
	endpoint          string
	reportedAddresses map[string]struct{}
	reason            string
}

type dockerProcess struct {
	Image   string `json:"Image"`
	Names   string `json:"Names"`
	Command string `json:"Command"`
}

// serverHostDetector is kept injectable so the ordering of the protection
// rules can be tested without needing a real panel process or Docker daemon.
var serverHostDetector = func() bool {
	return hasKomariServerProcess() || hasKomariDockerContainer()
}

func RemoteControlBlockedReason(endpoint string) string {
	if remoteIntegrationBypass() {
		return ""
	}
	now := time.Now()
	endpoint = strings.TrimSpace(endpoint)
	detectionCache.Lock()
	defer detectionCache.Unlock()
	if detectionCache.endpoint == endpoint && now.Sub(detectionCache.checkedAt) < cacheDuration {
		return detectionCache.reason
	}
	detectionCache.checkedAt = now
	detectionCache.endpoint = endpoint
	detectionCache.reason = detectKomariServer(endpoint)
	return detectionCache.reason
}

// SetReportedAddresses records the public or interface addresses that the Agent
// reports to Lite. This also covers NAT and container setups where a Server's
// public address is not assigned directly to an Agent-visible interface.
func SetReportedAddresses(addresses ...string) {
	normalized := make(map[string]struct{}, len(addresses))
	for _, value := range addresses {
		if ip := net.ParseIP(strings.TrimSpace(value)); ip != nil {
			normalized[ip.String()] = struct{}{}
		}
	}
	// A transient public-IP lookup failure must not clear a previously confirmed
	// protection signal. The next successful basic-info report refreshes it.
	if len(normalized) == 0 {
		return
	}

	detectionCache.Lock()
	defer detectionCache.Unlock()
	if equalAddressSets(detectionCache.reportedAddresses, normalized) {
		return
	}
	detectionCache.reportedAddresses = normalized
	detectionCache.checkedAt = time.Time{}
}

func detectKomariServer(endpoint string) string {
	// A node that hosts the Lite Server is an intentional remote-management
	// target. Do this check before endpoint/address matching because the Server
	// endpoint necessarily resolves back to the same node in this deployment.
	if serverHostDetector() {
		return ""
	}
	if endpointTargetsLocalAddress(endpoint) {
		return "Remote control is disabled because the Lite Server endpoint resolves to this node"
	}
	if endpointTargetsReportedAddress(endpoint, detectionCache.reportedAddresses) {
		return "Remote control is disabled because the Lite Server endpoint matches this node's reported address"
	}
	return ""
}

func endpointTargetsReportedAddress(endpoint string, reported map[string]struct{}) bool {
	if len(reported) == 0 {
		return false
	}
	resolved := resolveEndpointAddresses(endpoint)
	for _, address := range resolved {
		if _, exists := reported[address.IP.String()]; exists {
			return true
		}
	}
	return false
}

func resolveEndpointAddresses(endpoint string) []net.IPAddr {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil {
		return nil
	}
	return resolved
}

func endpointTargetsLocalAddress(endpoint string) bool {
	resolved := resolveEndpointAddresses(endpoint)
	if len(resolved) == 0 {
		return false
	}
	local := make(map[string]struct{})
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		if ip, _, parseErr := net.ParseCIDR(address.String()); parseErr == nil {
			local[ip.String()] = struct{}{}
		}
	}
	for _, address := range resolved {
		if address.IP.IsLoopback() {
			return true
		}
		if _, exists := local[address.IP.String()]; exists {
			return true
		}
	}
	return false
}

func equalAddressSets(left, right map[string]struct{}) bool {
	if len(left) != len(right) {
		return false
	}
	for address := range left {
		if _, exists := right[address]; !exists {
			return false
		}
	}
	return true
}

func hasKomariServerProcess() bool {
	processes, err := process.Processes()
	if err != nil {
		return false
	}
	currentPID := int32(os.Getpid())
	for _, candidate := range processes {
		if candidate.Pid == currentPID {
			continue
		}
		name, _ := candidate.Name()
		executable, _ := candidate.Exe()
		if isKomariServerProcess(name) || isKomariServerProcess(filepath.Base(executable)) {
			return true
		}
	}
	return false
}

func isKomariServerProcess(value string) bool {
	name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".exe")
	return name == "lite" || name == "lite-server" || name == "komari" || name == "komari-server"
}

func hasKomariDockerContainer() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "docker", "ps", "--format", "{{json .}}")
	output, err := command.Output()
	if err != nil {
		return false
	}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		var container dockerProcess
		if json.Unmarshal(scanner.Bytes(), &container) == nil && isKomariServerContainer(container) {
			return true
		}
	}
	return false
}

func isKomariServerContainer(container dockerProcess) bool {
	image := strings.ToLower(strings.TrimSpace(container.Image))
	name := strings.ToLower(strings.TrimSpace(container.Names))
	command := strings.ToLower(strings.TrimSpace(container.Command))
	if strings.Contains(image, "komari-agent") || strings.Contains(name, "komari-agent") ||
		strings.Contains(image, "lite-agent") || strings.Contains(name, "lite-agent") {
		return false
	}
	if digest := strings.IndexByte(image, '@'); digest >= 0 {
		image = image[:digest]
	}
	if slash, colon := strings.LastIndexByte(image, '/'), strings.LastIndexByte(image, ':'); colon > slash {
		image = image[:colon]
	}
	return image == "lite" || strings.HasSuffix(image, "/lite") ||
		name == "lite" || strings.HasPrefix(name, "lite-server") ||
		image == "komari" || strings.HasSuffix(image, "/komari") ||
		name == "komari" || strings.HasPrefix(name, "komari-server") ||
		(strings.Contains(command, "lite.exe") && !strings.Contains(command, "lite-agent")) ||
		(strings.Contains(command, "komari") && !strings.Contains(command, "komari-agent"))
}
