package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testConfig() config {
	return config{Listen: "100.64.0.10:18798", GatewayIP: "100.64.0.20", PublicOrigin: testOrigin, ExpectedExe: `C:\Users\ExampleUser\AppData\Local\Programs\openmausbot\OpenMausBot.exe`}
}
func gatewayRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, testOrigin+path, nil)
	r.RemoteAddr = "100.64.0.20:12345"
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}
func TestEdgeGatesBeforeDial(t *testing.T) {
	h, err := newBridge(testConfig(), func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("unauthorized request reached private pipe")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*http.Request)
		status int
	}{
		{"peer", func(r *http.Request) { r.RemoteAddr = "100.64.0.30:123" }, 403},
		{"host", func(r *http.Request) { r.Host = "other.example" }, 421},
		{"scheme", func(r *http.Request) { r.Header.Set("X-Forwarded-Proto", "http") }, 403},
		{"duplicate scheme", func(r *http.Request) { r.Header.Add("X-Forwarded-Proto", "https") }, 403},
		{"browser", func(r *http.Request) { r.Header.Set("Origin", testOrigin) }, 403},
		{"control path", func(r *http.Request) { r.URL.Path = "/state" }, 404},
		{"encrypted transport", func(r *http.Request) { r.URL.Path = "/v1/e2e" }, 404},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := gatewayRequest("GET", "/api/health")
			tc.change(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d", w.Code)
			}
		})
	}
}
func TestNativeAuthenticationAndPrivateHeaderBoundary(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-opaque" {
			t.Error("native bearer changed")
		}
		if r.Header.Get("X-Openmausbot-Control-Token") != "" || r.Header.Get("X-Openmausbot-Companion-Device") != "" {
			t.Error("private authority forwarded")
		}
		if r.Header.Get("X-Forwarded-For") != "" {
			t.Error("untrusted forwarding header retained")
		}
		if r.Header.Get("X-Forwarded-Proto") != "https" {
			t.Error("wrong transport metadata")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		io.WriteString(w, `{"error":"native-auth-denied","endpoints":[]}`)
	}))
	defer upstream.Close()
	h, err := newBridge(testConfig(), func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", upstream.Listener.Addr().String())
	})
	if err != nil {
		t.Fatal(err)
	}
	r := gatewayRequest("GET", "/api/companion/endpoints")
	r.Header.Set("Authorization", "Bearer test-opaque")
	r.Header.Set("X-Openmausbot-Control-Token", "must-not-forward")
	r.Header.Set("X-Openmausbot-Companion-Device", "forged")
	r.Header.Set("X-Forwarded-For", "100.64.0.20")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || w.Body.String() != `{"error":"native-auth-denied","endpoints":[]}` {
		t.Fatal("native denial altered")
	}
}
func TestPairAndEndpointResponsesUseHTTPS(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("compressed metadata requested")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/pair" {
			w.WriteHeader(201)
			io.WriteString(w, `{"token":"test-token","device":{"id":"test"},"serverName":"W","hosts":["192.0.2.18"]}`)
		} else {
			io.WriteString(w, `{"serverName":"W","endpoints":[]}`)
		}
	}))
	defer upstream.Close()
	h, _ := newBridge(testConfig(), func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", upstream.Listener.Addr().String())
	})
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"POST", "/api/pair", 201}, {"GET", "/api/companion/endpoints", 200}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, gatewayRequest(tc.method, tc.path))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), testOrigin) {
			t.Fatalf("metadata route failed: %d", w.Code)
		}
	}
}
func TestUnavailableNeverUsesTCPFallbackOrLogsRequest(t *testing.T) {
	calls := 0
	h, _ := newBridge(testConfig(), func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("sensitive-do-not-log")
	})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, gatewayRequest("GET", "/api/health?token=private-test"))
	if calls != 1 || w.Code != 503 || strings.Contains(w.Body.String(), "sensitive") || strings.Contains(w.Body.String(), "private-test") {
		t.Fatal("unavailable boundary failed")
	}
}
func TestConfigRejectsUnsafeOriginAndBinds(t *testing.T) {
	for _, origin := range []string{"http://phone.example.com", "https://user:pass@phone.example.com", "https://phone.example.com/path", "https://100.64.0.10", "https://phone.example.com?query=1"} {
		c := testConfig()
		c.PublicOrigin = origin
		if _, err := c.validate(); err == nil {
			t.Error("unsafe origin accepted")
		}
	}
	c := testConfig()
	c.Listen = "0.0.0.0:18798"
	if _, err := c.validate(); err == nil {
		t.Fatal("wildcard listener accepted")
	}
}
