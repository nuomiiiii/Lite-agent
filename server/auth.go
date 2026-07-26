package server

import (
	"net/http"

	"github.com/komari-monitor/komari-agent/requestheaders"
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
	headers.Set("X-Komari-Terminal-Session", sessionID)
	return headers
}

func remoteAuthorizationHeader(token, sessionID, ticket string) http.Header {
	headers := agentAuthorizationHeader(token)
	headers.Set("X-Komari-Remote-Session", sessionID)
	headers.Set("X-Komari-Remote-Ticket", ticket)
	return headers
}
