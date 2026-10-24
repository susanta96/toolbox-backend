package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func keyFor(t *testing.T, trust bool, remoteAddr string, headers map[string]string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	key, err := clientIPKey(trust)(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return key
}

func TestClientIPKey_HeaderIgnoredWhenNotTrusted(t *testing.T) {
	got := keyFor(t, false, "10.0.0.5:4321", map[string]string{"X-Real-IP": "203.0.113.9"})
	if got != "10.0.0.5" {
		t.Errorf("key = %q, want 10.0.0.5 (host of RemoteAddr)", got)
	}
}

func TestClientIPKey_HeaderHonouredWhenTrusted(t *testing.T) {
	got := keyFor(t, true, "10.0.0.5:4321", map[string]string{"X-Real-IP": "203.0.113.9"})
	if got != "203.0.113.9" {
		t.Errorf("key = %q, want 203.0.113.9", got)
	}
}

func TestClientIPKey_IgnoresTrueClientIPAndForwardedFor(t *testing.T) {
	got := keyFor(t, true, "10.0.0.5:4321", map[string]string{
		"True-Client-IP":  "198.51.100.1",
		"X-Forwarded-For": "198.51.100.2",
	})
	if got != "10.0.0.5" {
		t.Errorf("key = %q, want 10.0.0.5", got)
	}
}

func TestClientIPKey_InvalidOrEmptyHeaderFallsBack(t *testing.T) {
	for _, hdr := range []string{"", "not-an-ip", "203.0.113.9, 1.2.3.4"} {
		got := keyFor(t, true, "10.0.0.5:4321", map[string]string{"X-Real-IP": hdr})
		if got != "10.0.0.5" {
			t.Errorf("X-Real-IP %q: key = %q, want 10.0.0.5", hdr, got)
		}
	}
}

func TestClientIPKey_StripsPortFromRemoteAddr(t *testing.T) {
	if got := keyFor(t, false, "192.0.2.7:5555", nil); got != "192.0.2.7" {
		t.Errorf("ipv4 key = %q", got)
	}
	if got := keyFor(t, false, "[2001:db8::1]:5555", nil); got != "2001:db8::1" {
		t.Errorf("ipv6 key = %q", got)
	}
	if got := keyFor(t, false, "192.0.2.7", nil); got != "192.0.2.7" {
		t.Errorf("portless key = %q", got)
	}
}
