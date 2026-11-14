package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type recordingTransport struct{ calls int }

func (t *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{StatusCode: 200, Body: http.NoBody, Header: make(http.Header)}, nil
}
func TestNativeTimeoutBudgetRouting(t *testing.T) {
	normal, secret := &recordingTransport{}, &recordingTransport{}
	transport := nativeBudgetTransport{normal, secret}
	for _, path := range []string{"/api/bots/bot/secret-cards/card/provide", "/api/bots/bot/secret-cards/card/provide?retry=1", "/api/health", "/api/bots/bot/secret-cards/card/provide/extra"} {
		_, err := transport.RoundTrip(httptest.NewRequest("POST", testOrigin+path, nil))
		if err != nil {
			t.Fatal(err)
		}
	}
	if normal.calls != 2 || secret.calls != 2 {
		t.Fatal("native secret-save timeout budget not selected correctly")
	}
}
