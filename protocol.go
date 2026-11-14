package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

// Preserve every non-routing field as raw JSON, including bearer credentials,
// device identity, pinned public keys, version and future protocol fields.
func rawObject(data []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return nil, errors.New("metadata must be an object")
	}
	out := make(map[string]json.RawMessage)
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok {
			return nil, errors.New("invalid metadata key")
		}
		if _, exists := out[k]; exists {
			return nil, errors.New("duplicate metadata key")
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return nil, errors.New("invalid metadata value")
		}
		out[k] = v
	}
	if _, e = d.Token(); e != nil {
		return nil, errors.New("invalid metadata object")
	}
	var trailing any
	if e = d.Decode(&trailing); e != io.EOF {
		return nil, errors.New("trailing metadata")
	}
	return out, nil
}
func normalizeMetadata(data []byte, origin string, pair bool) ([]byte, error) {
	m, err := rawObject(data)
	if err != nil {
		return nil, err
	}
	var name string
	if json.Unmarshal(m["serverName"], &name) != nil || name == "" {
		return nil, errors.New("missing native server identity")
	}
	if e, exists := m["error"]; exists && string(e) != "null" {
		return nil, errors.New("upstream error is not metadata")
	}
	if e, exists := m["e2e"]; exists && string(bytes.TrimSpace(e)) != "null" {
		return nil, errors.New("pinned encrypted transport requires a native compatible route")
	}
	if pair {
		var device map[string]json.RawMessage
		var id string
		if json.Unmarshal(m["device"], &device) != nil || json.Unmarshal(device["id"], &id) != nil || id == "" {
			return nil, errors.New("missing authenticated native pairing result")
		}
		// Do not send a new phone's bearer to an unrelated 192.168 host on another Wi-Fi.
		m["hosts"] = json.RawMessage(`[]`)
	}
	routes, err := json.Marshal([]map[string]any{{"url": origin, "kind": "hosted", "priority": 0}})
	if err != nil {
		return nil, err
	}
	m["endpoints"] = routes
	return json.Marshal(m)
}

const maxEventFrame = 4 << 20

// Unknown SSE frames are byte-for-byte passthrough. Only the sidecar-owned
// endpoint announcement is rewritten, after upstream authenticated the stream.
func normalizeEventFrame(frame []byte, origin string) ([]byte, error) {
	lines := bytes.SplitAfter(frame, []byte("\n"))
	var data []string
	for _, line := range lines {
		clean := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if strings.HasPrefix(clean, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(clean, "data:"), " "))
		}
	}
	if len(data) == 0 {
		return frame, nil
	}
	payload := []byte(strings.Join(data, "\n"))
	var kind struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(payload, &kind) != nil || kind.Kind != "companion.endpoints" {
		return frame, nil
	}
	rewritten, err := normalizeMetadata(payload, origin, false)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	replaced := false
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte("data:")) {
			if !replaced {
				ending := ""
				if bytes.HasSuffix(line, []byte("\r\n")) {
					ending = "\r\n"
				} else if bytes.HasSuffix(line, []byte("\n")) {
					ending = "\n"
				}
				out.WriteString("data: ")
				out.Write(rewritten)
				out.WriteString(ending)
				replaced = true
			}
		} else {
			out.Write(line)
		}
	}
	return out.Bytes(), nil
}

type eventReader struct {
	*io.PipeReader
	upstream io.ReadCloser
	once     sync.Once
}

func (e *eventReader) Close() error {
	e.once.Do(func() { _ = e.upstream.Close(); _ = e.PipeReader.Close() })
	return nil
}
func normalizeEvents(upstream io.ReadCloser, origin string) io.ReadCloser {
	pr, pw := io.Pipe()
	result := &eventReader{PipeReader: pr, upstream: upstream}
	go func() {
		defer upstream.Close()
		defer pw.Close()
		reader := bufio.NewReaderSize(upstream, 32<<10)
		var frame bytes.Buffer
		var line bytes.Buffer
		fail := func() { _ = pw.CloseWithError(errors.New("invalid upstream endpoint stream")) }
		flush := func() bool {
			out, err := normalizeEventFrame(frame.Bytes(), origin)
			if err != nil {
				fail()
				return false
			}
			if _, err = pw.Write(out); err != nil {
				return false
			}
			frame.Reset()
			return true
		}
		for {
			part, err := reader.ReadSlice('\n')
			if frame.Len()+line.Len()+len(part) > maxEventFrame {
				fail()
				return
			}
			line.Write(part)
			if err == bufio.ErrBufferFull {
				continue
			}
			if line.Len() > 0 {
				complete := line.Bytes()
				blank := bytes.Equal(complete, []byte("\n")) || bytes.Equal(complete, []byte("\r\n"))
				frame.Write(complete)
				line.Reset()
				if blank && !flush() {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					fail()
					return
				}
				if frame.Len() > 0 {
					_ = flush()
				}
				return
			}
		}
	}()
	return result
}
