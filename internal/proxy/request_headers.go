package proxy

import (
	"net/http"
	"strings"
)

var requestHeaderOrder = []string{
	"authorization",
	"Referer",
	"user-agent",
	"content-type",
	"accept",
	"accept-language",
	"x-zcode-app-version",
	"x-title",
	"x-zcode-agent",
	"x-session-id",
	"x-request-id",
	"x-zcode-trace-id",
	"x-query-id",
}

func setHeadersInOrder(request *http.Request, headers map[string]string) {
	seen := make(map[string]bool, len(requestHeaderOrder))
	for _, name := range requestHeaderOrder {
		if value := headers[name]; value != "" {
			request.Header.Set(name, value)
			seen[strings.ToLower(name)] = true
		}
	}
	for name, value := range headers {
		if !seen[strings.ToLower(name)] && value != "" {
			request.Header.Set(name, value)
		}
	}
}
