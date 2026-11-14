// A separately installed, tailnet-only compatibility proxy. It never changes
// OpenMausBot, reads its credentials, or bypasses its authentication.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"time"
)

type config struct {
	Listen       string `json:"listen"`
	GatewayIP    string `json:"gateway_ip"`
	PublicOrigin string `json:"public_origin"`
	ExpectedExe  string `json:"expected_exe"`
}

func (c *config) validate() (*url.URL, error) {
	host, port, err := net.SplitHostPort(c.Listen)
	if err != nil || port != "18798" || !isTailnetIP(net.ParseIP(host)) {
		return nil, errors.New("listen must be a tailnet IP on port 18798")
	}
	if !isTailnetIP(net.ParseIP(c.GatewayIP)) {
		return nil, errors.New("gateway_ip must be a literal tailnet IP")
	}
	u, err := url.Parse(c.PublicOrigin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.Port() != "" || net.ParseIP(u.Hostname()) != nil {
		return nil, errors.New("public_origin must be a bare DNS HTTPS origin with default port")
	}
	if strings.ContainsAny(u.Host, "\r\n\t ") || strings.Contains(u.Host, ":") {
		return nil, errors.New("invalid public hostname")
	}
	for _, r := range u.Host {
		if r > 127 {
			return nil, errors.New("public hostname must be ASCII")
		}
	}
	if c.ExpectedExe == "" {
		return nil, errors.New("expected_exe is required")
	}
	c.PublicOrigin = "https://" + strings.ToLower(u.Host)
	u.Host = strings.ToLower(u.Host)
	u.Path = ""
	return u, nil
}
func isTailnetIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v := ip.To4(); v != nil {
		return v[0] == 100 && v[1] >= 64 && v[1] <= 127
	}
	_, prefix, _ := net.ParseCIDR("fd7a:115c:a1e0::/48")
	return prefix.Contains(ip)
}

func publicError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// The dialing callback is injected only for unit tests. Production supplies the
// Windows verified private-pipe dialer; there is no TCP upstream option.
func newBridge(c config, dial func(context.Context, string, string) (net.Conn, error)) (http.Handler, error) {
	origin, err := c.validate()
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext:            dial,
		DisableKeepAlives:      true, // Revalidate process identity on every new request.
		DisableCompression:     true,
		ForceAttemptHTTP2:      false,
		ResponseHeaderTimeout:  30 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
	}
	secretTransport := transport.Clone()
	secretTransport.ResponseHeaderTimeout = 105 * time.Second
	proxy := &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.Out.URL.Scheme = "http"
			p.Out.URL.Host = "companion.invalid"
			p.Out.Host = origin.Host
			// Neither caller-supplied forwarding headers nor private desktop authority
			// headers acquire trust through this bridge. Native bearer auth is retained.
			for k := range p.Out.Header {
				kl := strings.ToLower(k)
				if strings.HasPrefix(kl, "x-openmausbot-") || strings.HasPrefix(kl, "x-forwarded-") || kl == "forwarded" {
					p.Out.Header.Del(k)
				}
			}
			p.Out.Header.Set("X-Forwarded-Proto", "https")
			p.Out.Header.Set("X-Forwarded-Host", origin.Host)
			if metadataPath(p.In.Method, p.In.URL.Path) || p.In.URL.Path == "/api/events" {
				p.Out.Header.Set("Accept-Encoding", "identity")
			}
		},
		Transport:     nativeBudgetTransport{normal: transport, secret: secretTransport},
		FlushInterval: -1,
		ModifyResponse: func(r *http.Response) error {
			if r.Request.Method == "POST" && r.Request.URL.Path == "/api/pair" {
				log.Printf("native pairing response: HTTP %d", r.StatusCode)
			}
			err := normalizeResponse(r, c.PublicOrigin)
			if err != nil && r.Request.Method == "POST" && r.Request.URL.Path == "/api/pair" {
				log.Print("native pairing metadata incompatible; response withheld")
			}
			return err
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, e error) {
			// Only a fixed diagnostic label: never e, URL, headers or body.
			if r.Method == "POST" && r.URL.Path == "/api/pair" {
				log.Print("pairing request unavailable before a valid response")
			}
			publicError(w, http.StatusServiceUnavailable, "Windows companion unavailable; keep MausBot open with phone access active")
		},
		ErrorLog: log.New(io.Discard, "", 0),
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(remote).Equal(net.ParseIP(c.GatewayIP)) {
			publicError(w, 403, "gateway only")
			return
		}
		if !strings.EqualFold(r.Host, origin.Host) && !strings.EqualFold(r.Host, origin.Host+":443") {
			publicError(w, 421, "unexpected host")
			return
		}
		proto := r.Header.Values("X-Forwarded-Proto")
		if len(proto) != 1 || proto[0] != "https" {
			publicError(w, 403, "HTTPS gateway required")
			return
		}
		// Preserve, rather than bypass, the companion's native-app-only boundary.
		if _, exists := r.Header["Origin"]; exists {
			publicError(w, 403, "browser origins are not allowed")
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			publicError(w, 404, "not found")
			return
		}
		proxy.ServeHTTP(w, r)
	}), nil
}

var secretProvidePath = regexp.MustCompile(`^/api/bots/[\w-]+/secret-cards/[\w-]+/provide$`)

type nativeBudgetTransport struct{ normal, secret http.RoundTripper }

func (t nativeBudgetTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if secretProvidePath.MatchString(r.URL.Path) {
		return t.secret.RoundTrip(r)
	}
	return t.normal.RoundTrip(r)
}

func metadataPath(method, path string) bool {
	return (method == "POST" && path == "/api/pair") || (method == "GET" && path == "/api/companion/endpoints")
}

func normalizeResponse(r *http.Response, origin string) error {
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return nil
	} // Never alter upstream auth failures.
	if metadataPath(r.Request.Method, r.Request.URL.Path) {
		if r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
			return errors.New("unexpected metadata encoding")
		}
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
			return errors.New("unexpected metadata format")
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
		_ = r.Body.Close()
		if err != nil || len(data) > 64<<10 {
			return errors.New("invalid metadata size")
		}
		out, err := normalizeMetadata(data, origin, r.Request.URL.Path == "/api/pair")
		if err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(out))
		r.ContentLength = int64(len(out))
		r.Header.Set("Content-Length", fmt.Sprint(len(out)))
		r.Header.Del("Content-Encoding")
		r.Header.Del("ETag")
		r.Header.Set("Cache-Control", "private, no-store")
	} else if r.Request.Method == "GET" && r.Request.URL.Path == "/api/events" {
		if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "text/event-stream") {
			return errors.New("unexpected event stream format")
		}
		if r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
			return errors.New("unexpected event stream encoding")
		}
		r.Body = normalizeEvents(r.Body, origin)
		r.ContentLength = -1
		r.Header.Del("Content-Length")
		r.Header.Del("ETag")
	}
	return nil
}

func main() {
	path := flag.String("config", "bridge.json", "private bridge config")
	selftest := flag.Bool("probe", false, "verify a private pipe and unauthenticated native health response, then exit")
	logPath := flag.String("log", "", "private service status log (never request data)")
	flag.Parse()
	if *logPath != "" {
		if info, e := os.Stat(*logPath); e == nil && info.Size() > 1<<20 {
			if os.Rename(*logPath, *logPath+".previous") != nil {
				log.Fatal("cannot rotate private status log")
			}
		}
		f, e := os.OpenFile(*logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			log.Fatal("cannot open private status log")
		}
		defer f.Close()
		log.SetOutput(f)
	}
	data, err := os.ReadFile(*path)
	if err != nil {
		log.Fatal("cannot read bridge configuration")
	}
	var c config
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		log.Fatal("invalid bridge configuration")
	}
	if _, err = c.validate(); err != nil {
		log.Fatal(err)
	}
	dial, err := newPipeDialer(c.ExpectedExe)
	if err != nil {
		log.Fatal(err)
	}
	if *selftest {
		client := &http.Client{Transport: &http.Transport{DialContext: dial, DisableKeepAlives: true, DisableCompression: true}, Timeout: 8 * time.Second}
		resp, err := client.Get("http://companion.invalid/api/health")
		if err != nil {
			log.Fatal("verified private pipe unavailable: ", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
		var v map[string]json.RawMessage
		if err != nil || len(body) > 4096 || resp.StatusCode != 200 || json.Unmarshal(body, &v) != nil || string(v["app"]) != "\"openmausbot\"" {
			log.Fatal("unexpected private companion health response")
		}
		log.Print("verified native companion private-pipe health: OK")
		return
	}
	handler, err := newBridge(c, dial)
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Addr: c.Listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		cancelCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(cancelCtx)
	}()
	log.Printf("private HTTPS compatibility bridge listening on %s; only gateway %s accepted", c.Listen, c.GatewayIP)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("bridge listener stopped")
	}
}
