package server

import (
	"net/http"

	"github.com/nuomiiiii/lite-agent/requestheaders"
)

func agentAuthorizationHeader(token string) http.Header {
	headers := make(http.Header)
	requestheaders.ApplyAgentAuthentication(headers, token, flags.CFAccessClientID, flags.CFAccessClientSecret)
	return headers
}

func authorizeAgentRequest(request *http.Request, token string) {
	requestheaders.ApplyAgentAuthentication(request.Header, token, flags.CFAccessClientID, flags.CFAccessClientSecret)
}

func terminalAuthorizationHeader(token, sessionID string) http.Header {
	headers := agentAuthorizationHeader(token)
	// Send both protocol names so this agent can be controlled by Lite Server
	// and remaining Komari Lite servers, including remote terminal.
	headers.Set("X-Komari-Terminal-Session", sessionID)
	headers.Set("X-Lite-Terminal-Session", sessionID)
	return headers
}

func remoteAuthorizationHeader(token, sessionID, ticket string) http.Header {
	headers := agentAuthorizationHeader(token)
	headers.Set("X-Komari-Remote-Session", sessionID)
	headers.Set("X-Komari-Remote-Ticket", ticket)
	headers.Set("X-Lite-Remote-Session", sessionID)
	headers.Set("X-Lite-Remote-Ticket", ticket)
	return headers
}
