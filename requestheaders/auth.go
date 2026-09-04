package requestheaders

import "net/http"

const (
	cloudflareAccessClientIDHeader     = "CF-Access-Client-Id"
	cloudflareAccessClientSecretHeader = "CF-Access-Client-Secret"
)

// ApplyCloudflareAccess adds Cloudflare Access service-token credentials when
// both values are configured. Partial credentials are deliberately ignored;
// runtime configuration validation reports that case before the agent starts.
func ApplyCloudflareAccess(headers http.Header, clientID, clientSecret string) {
	if clientID == "" || clientSecret == "" {
		return
	}
	headers.Set(cloudflareAccessClientIDHeader, clientID)
	headers.Set(cloudflareAccessClientSecretHeader, clientSecret)
}

// ApplyAgentAuthentication keeps Lite agent authentication separate from the
// Cloudflare Access service token. Both authentication layers can coexist on
// the same HTTP or WebSocket handshake.
func ApplyAgentAuthentication(headers http.Header, token, clientID, clientSecret string) {
	headers.Set("Authorization", "Bearer "+token)
	ApplyCloudflareAccess(headers, clientID, clientSecret)
}
