package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

const testOrigin = "https://phone.example.com"

func TestMetadataKeepsCredentialsAndIdentity(t *testing.T) {
	input := []byte(`{"token":"opaque-test-token","device":{"id":"phone-test","routes":"local"},"serverName":"Windows","secretKey":"unchanged-test-key","version":"0.1.158","extra":{"integer":9007199254740993},"hosts":["192.0.2.18"],"endpoints":[{"url":"http://192.0.2.18:8810","kind":"lan","priority":0}]}`)
	output, err := normalizeMetadata(input, testOrigin, true)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := rawObject(input)
	after, _ := rawObject(output)
	for _, key := range []string{"token", "device", "serverName", "secretKey", "version", "extra"} {
		if !bytes.Equal(before[key], after[key]) {
			t.Fatalf("non-routing field changed: %s", key)
		}
	}
	var eps []map[string]any
	if json.Unmarshal(after["endpoints"], &eps) != nil || len(eps) != 1 || eps[0]["url"] != testOrigin || eps[0]["kind"] != "hosted" {
		t.Fatal("incorrect HTTPS advertisement")
	}
	if string(after["hosts"]) != "[]" {
		t.Fatal("LAN bearer fallback remains")
	}
}
func TestMetadataFailsClosed(t *testing.T) {
	for _, input := range []string{
		`{"serverName":"W","serverName":"X"}`,
		`{"serverName":"W","e2e":{"v":1,"k":"pinned"}}`,
		`{"serverName":"W","error":"unauthorized"}`,
		`{"serverName":"W"} {}`,
		`{"serverName":""}`,
	} {
		if _, err := normalizeMetadata([]byte(input), testOrigin, false); err == nil {
			t.Errorf("unsafe metadata accepted: %s", input)
		}
	}
	if _, err := normalizeMetadata([]byte(`{"serverName":"W"}`), testOrigin, true); err == nil {
		t.Fatal("not a native pairing result")
	}
}
func TestSSEOnlyEndpointAnnouncementsChange(t *testing.T) {
	ordinary := ": keepalive\r\n\r\nid: 23\ndata: {\"kind\":\"token\",\"text\":\"unchanged\"}\n\n"
	announcement := "event: update\r\ndata: {\"kind\":\"companion.endpoints\",\"serverName\":\"W\",\"version\":\"0.1.158\",\"endpoints\":[]}\r\n\r\n"
	body := normalizeEvents(io.NopCloser(strings.NewReader(ordinary+announcement)), testOrigin)
	defer body.Close()
	out, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte(ordinary)) {
		t.Fatal("ordinary events changed")
	}
	if !bytes.Contains(out, []byte(testOrigin)) || !bytes.Contains(out, []byte("event: update\r\n")) || !bytes.HasSuffix(out, []byte("\r\n\r\n")) {
		t.Fatal("bad endpoint event")
	}
}
func TestSSEPinnedTransportAndSizeFailClosed(t *testing.T) {
	for _, input := range []string{
		"data: {\"kind\":\"companion.endpoints\",\"serverName\":\"W\",\"e2e\":{\"v\":1}}\n\n",
		"data: " + strings.Repeat("x", maxEventFrame) + "\n\n",
	} {
		body := normalizeEvents(io.NopCloser(strings.NewReader(input)), testOrigin)
		_, err := io.ReadAll(body)
		body.Close()
		if err == nil {
			t.Fatal("unsafe stream accepted")
		}
	}
}
