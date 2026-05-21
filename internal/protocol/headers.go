package protocol

import (
	"net/http"
	"strings"
)

func ApplyClientUserAgent(req *http.Request, headers map[string][]string) {
	for key, values := range headers {
		if !strings.EqualFold(key, "User-Agent") {
			continue
		}
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				req.Header.Set("User-Agent", trimmed)
				return
			}
		}
	}

	req.Header["User-Agent"] = nil
}
