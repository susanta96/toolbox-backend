package handler

import (
	"net"
	"net/http"
)

// clientIPKey returns an httprate key function that identifies the client by IP.
//
// By default it uses the host part of RemoteAddr. When trustProxyHeader is true
// the API sits behind a reverse proxy that sets X-Real-IP, so that header is
// used instead, provided it holds a single valid IP; otherwise it falls back to
// RemoteAddr. Client-controllable headers such as True-Client-IP and
// X-Forwarded-For are never consulted.
func clientIPKey(trustProxyHeader bool) func(*http.Request) (string, error) {
	return func(r *http.Request) (string, error) {
		if trustProxyHeader {
			if ip := net.ParseIP(r.Header.Get("X-Real-IP")); ip != nil {
				return ip.String(), nil
			}
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return r.RemoteAddr, nil
		}
		return host, nil
	}
}
