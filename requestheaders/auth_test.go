package requestheaders

import (
	"net/http"
	"testing"
)

func TestAgentAndCloudflareAuthenticationCoexist(t *testing.T) {
	headers := make(http.Header)
	ApplyAgentAuthentication(headers, "agent-token", "access-id", "access-secret")

	if got := headers.Get("Authorization"); got != "Bearer agent-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := headers.Get("CF-Access-Client-Id"); got != "access-id" {
		t.Fatalf("CF-Access-Client-Id = %q", got)
	}
	if got := headers.Get("CF-Access-Client-Secret"); got != "access-secret" {
		t.Fatalf("CF-Access-Client-Secret = %q", got)
	}
}

func TestCloudflareAccessRequiresCompleteCredentials(t *testing.T) {
	for _, credentials := range []struct {
		clientID     string
		clientSecret string
	}{
		{},
		{clientID: "access-id"},
		{clientSecret: "access-secret"},
	} {
		headers := make(http.Header)
		ApplyCloudflareAccess(headers, credentials.clientID, credentials.clientSecret)
		if headers.Get("CF-Access-Client-Id") != "" || headers.Get("CF-Access-Client-Secret") != "" {
			t.Fatalf("partial Cloudflare Access credentials were sent: %#v", headers)
		}
	}
}
