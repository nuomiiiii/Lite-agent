//go:build komari_remote_integration || lite_remote_integration

package hostguard

func remoteIntegrationBypass() bool {
	return true
}
