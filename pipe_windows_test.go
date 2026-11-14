//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const pipeFixtureEnv = "BRIDGE_PRIVATE_PIPE_TEST_FIXTURE"

type pipeFixtureConfig struct {
	Mode, Path, Stop, ChildStop string
}

type pipeFixtureMessage struct {
	Kind string
	PID  int
	Path string
	N    int64
}

// The fixture protocol contains only readiness, PIDs and byte counts. No test
// writes credentials (or any application bytes) to a candidate pipe.
func TestPipeFixture(t *testing.T) {
	value := os.Getenv(pipeFixtureEnv)
	if value == "" {
		return
	}
	var cfg pipeFixtureConfig
	if err := json.Unmarshal([]byte(value), &cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := runPipeFixture(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	// Do not mix the Go test runner's output with the JSON readiness protocol.
	os.Exit(0)
}

func pipeFixtureCommand(cfg pipeFixtureConfig) (*exec.Cmd, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	value, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, "-test.run=^TestPipeFixture$", "-test.timeout=5s")
	// Replace rather than append: the main fixture itself already has this key.
	for _, entry := range os.Environ() {
		if !strings.EqualFold(strings.SplitN(entry, "=", 2)[0], pipeFixtureEnv) {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, pipeFixtureEnv+"="+string(value))
	return cmd, nil
}

func runPipeFixture(cfg pipeFixtureConfig) error {
	name, err := windows.UTF16PtrFromString(cfg.Stop)
	if err != nil {
		return err
	}
	stop, err := windows.OpenEvent(windows.SYNCHRONIZE, false, name)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(stop)
	if cfg.Mode == "main" {
		cfg.Mode, cfg.Stop = "server", cfg.ChildStop
		cfg.Path = fmt.Sprintf(cfg.Path, os.Getpid())
		child, err := pipeFixtureCommand(cfg)
		if err != nil {
			return err
		}
		// Relay just readiness, not inherited stdout/stderr handles: otherwise
		// exec.Cmd.Wait in the test could wait for the still-live grandchild.
		stdout, err := child.StdoutPipe()
		if err != nil {
			return err
		}
		if err := child.Start(); err != nil {
			_ = stdout.Close()
			return err
		}
		var ready pipeFixtureMessage
		err = json.NewDecoder(stdout).Decode(&ready)
		_ = stdout.Close()
		if err == nil {
			err = json.NewEncoder(os.Stdout).Encode(ready)
		}
		if err != nil {
			_ = child.Process.Kill()
			_ = child.Wait()
			return err
		}
		state, waitErr := windows.WaitForSingleObject(stop, 5000)
		if waitErr != nil || state != windows.WAIT_OBJECT_0 {
			_ = child.Process.Kill() // Only the fixture we just created.
			_ = child.Wait()
			return fmt.Errorf("fixture main stop: state=%d err=%v", state, waitErr)
		}
		// Deliberately leave the server alive. The test holds a generation-bound
		// handle and owns its independent stop event, including failure cleanup.
		return child.Process.Release()
	}
	if cfg.Mode != "server" {
		return fmt.Errorf("unknown fixture mode %q", cfg.Mode)
	}
	listener, err := winio.ListenPipe(cfg.Path, nil)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err := json.NewEncoder(os.Stdout).Encode(pipeFixtureMessage{Kind: "ready", PID: os.Getpid(), Path: cfg.Path}); err != nil {
		return err
	}
	type acceptedPipe struct {
		conn net.Conn
	}
	accepted := make(chan acceptedPipe, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		conn, err := listener.Accept()
		accepted <- acceptedPipe{conn: conn}
		if err != nil {
			return
		}
		defer conn.Close()
		n, _ := io.Copy(io.Discard, conn)
		_ = json.NewEncoder(os.Stdout).Encode(pipeFixtureMessage{Kind: "observed", PID: os.Getpid(), N: n})
	}()
	state, waitErr := windows.WaitForSingleObject(stop, 5000)
	_ = listener.Close() // Unblocks Accept even if the test never dialed.
	a := <-accepted
	if a.conn != nil {
		_ = a.conn.Close() // Unblocks the read before joining its goroutine.
	}
	<-readDone
	if waitErr != nil || state != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("fixture server stop: state=%d err=%v", state, waitErr)
	}
	return nil
}

func pipeTestUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6], b[8] = b[6]&15|64, b[8]&63|128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type pipeTestEvent struct {
	name string
	h    windows.Handle
}

func newPipeTestEvent(t *testing.T) pipeTestEvent {
	t.Helper()
	e := pipeTestEvent{name: `Local\bridge-pipe-test-` + pipeTestUUID(t)}
	name, err := windows.UTF16PtrFromString(e.name)
	if err != nil {
		t.Fatal(err)
	}
	e.h, err = windows.CreateEvent(nil, 1, 0, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(e.h) })
	return e
}

func (e pipeTestEvent) signal(t *testing.T) {
	t.Helper()
	if err := windows.SetEvent(e.h); err != nil {
		t.Error(err)
	}
}

type pipeTestProcess struct {
	cmd      *exec.Cmd
	messages chan pipeFixtureMessage
	scanned  chan struct{}
	waited   chan struct{}
	waitErr  error // Read only after waited is closed.
	stderr   bytes.Buffer
}

func startPipeTestProcess(t *testing.T, cfg pipeFixtureConfig, stop pipeTestEvent) *pipeTestProcess {
	t.Helper()
	cmd, err := pipeFixtureCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := &pipeTestProcess{cmd: cmd, messages: make(chan pipeFixtureMessage, 8), scanned: make(chan struct{}), waited: make(chan struct{})}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = &p.stderr
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		t.Fatal(err)
	}
	go func() {
		defer close(p.scanned)
		defer close(p.messages)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var msg pipeFixtureMessage
			if json.Unmarshal(scanner.Bytes(), &msg) != nil {
				msg.Kind = "invalid fixture output"
			}
			p.messages <- msg // At most ready + observed, so never blocks.
		}
	}()
	go func() {
		p.waitErr = cmd.Wait()
		close(p.waited)
	}()
	t.Cleanup(func() {
		stop.signal(t)
		select {
		case <-p.waited:
		case <-time.After(3 * time.Second):
			t.Error("fixture did not exit after its stop event")
			_ = cmd.Process.Kill() // Never operates on a discovered/native PID.
			select {
			case <-p.waited:
			case <-time.After(5 * time.Second):
				t.Error("could not reap test-owned fixture")
			}
		}
		_ = stdout.Close()
		select {
		case <-p.scanned:
		case <-time.After(3 * time.Second):
			t.Error("fixture stdout reader did not stop")
		}
		select {
		case <-p.waited:
			if p.waitErr != nil {
				t.Errorf("fixture failed: %v: %s", p.waitErr, p.stderr.String())
			}
		default:
		}
	})
	return p
}

func (p *pipeTestProcess) message(t *testing.T, kind string) pipeFixtureMessage {
	t.Helper()
	select {
	case msg, ok := <-p.messages:
		if !ok || msg.Kind != kind {
			t.Fatalf("fixture: wanted %s, got %+v (open=%v)", kind, msg, ok)
		}
		return msg
	case <-time.After(5 * time.Second):
		t.Fatalf("fixture did not report %s within 5s", kind)
		return pipeFixtureMessage{}
	}
}

func pipeTestIdentity(t *testing.T) (string, string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonicalPipeImage(exe)
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	return expected, user.User.Sid.String()
}

func dialPipeTest(t *testing.T, path string) (net.Conn, windows.Filetime) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var before windows.Filetime
	windows.GetSystemTimeAsFileTime(&before)
	conn, err := winio.DialPipeContext(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, before
}

func ownVerifiedPipe(t *testing.T, conn *verifiedPipeConn) {
	t.Helper()
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.checkAlive(); err != nil {
		t.Fatal(err)
	}
}

func requirePipeMonitorClosed(t *testing.T, conn *verifiedPipeConn) {
	t.Helper()
	select {
	case <-conn.done:
	case <-time.After(3 * time.Second):
		t.Fatal("production generation monitor did not close connection within 3s")
	}
	if err := conn.checkAlive(); err == nil {
		t.Error("closed connection still reports alive")
	}
	if n, err := conn.Write([]byte("must-not-be-written")); n != 0 || err == nil {
		t.Errorf("write after exit: n=%d err=%v", n, err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err == nil {
		var b [1]byte
		if _, err := conn.Read(b[:]); err == nil {
			t.Error("read succeeded after monitor closed pipe")
		}
	}
}

func TestOriginPipeRejectsImpostorWithoutApplicationBytes(t *testing.T) {
	stop := newPipeTestEvent(t)
	path := fmt.Sprintf("%s%d-%s", originPipePrefix, os.Getpid(), pipeTestUUID(t))
	p := startPipeTestProcess(t, pipeFixtureConfig{Mode: "server", Path: path, Stop: stop.name}, stop)
	ready := p.message(t, "ready")
	if ready.Path != path || ready.PID != p.cmd.Process.Pid || ready.PID == os.Getpid() {
		t.Fatalf("incorrect fixture identity: %+v", ready)
	}
	_, sid := pipeTestIdentity(t)
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonicalPipeImage(filepath.Join(systemDir, "cmd.exe"))
	if err != nil {
		t.Fatal(err)
	}
	raw, before := dialPipeTest(t, path)
	verified, err := verifyOriginPipe(raw, path, expected, sid, before)
	if verified != nil {
		_ = verified.Close()
		t.Fatal("impostor was accepted")
	}
	if err == nil || !strings.Contains(err.Error(), "image mismatch") {
		t.Fatalf("wanted executable mismatch, got %v", err)
	}
	_ = raw.Close()
	if msg := p.message(t, "observed"); msg.N != 0 {
		t.Fatalf("impostor received %d application bytes before rejection", msg.N)
	}
}

func TestOriginPipeChildExitClosesVerifiedConnection(t *testing.T) {
	stop := newPipeTestEvent(t)
	path := fmt.Sprintf("%s%d-%s", originPipePrefix, os.Getpid(), pipeTestUUID(t))
	p := startPipeTestProcess(t, pipeFixtureConfig{Mode: "server", Path: path, Stop: stop.name}, stop)
	ready := p.message(t, "ready")
	if ready.Path != path || ready.PID != p.cmd.Process.Pid {
		t.Fatalf("incorrect fixture identity: %+v", ready)
	}
	expected, sid := pipeTestIdentity(t)
	raw, before := dialPipeTest(t, path)
	// A real timestamp at/before server creation must fail the generation gate.
	server, err := pinPipeProcess(uint32(ready.PID), expected, sid)
	if err != nil {
		t.Fatal(err)
	}
	created := server.created
	_ = windows.CloseHandle(server.handle)
	bad, err := verifyOriginPipe(raw, path, expected, sid, created)
	if bad != nil {
		_ = bad.Close()
		t.Fatal("server created at beforeDial boundary was accepted")
	}
	if err == nil || !strings.Contains(err.Error(), "generation mismatch") {
		t.Fatalf("wanted generation mismatch, got %v", err)
	}
	verified, err := verifyOriginPipe(raw, path, expected, sid, before)
	if err != nil {
		t.Fatal(err)
	}
	ownVerifiedPipe(t, verified)
	stamp := func(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
	if !(stamp(verified.main.created) < stamp(verified.server.created) && stamp(verified.server.created) < stamp(before)) {
		t.Fatal("expected main creation < server creation < pre-dial time")
	}
	stop.signal(t)
	requirePipeMonitorClosed(t, verified)
	select {
	case <-p.waited:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture child did not exit")
	}
}

func TestOriginPipeMainExitClosesWhileServerLives(t *testing.T) {
	mainStop, serverStop := newPipeTestEvent(t), newPipeTestEvent(t)
	// Only the required validator namespace is used, with an unpredictable UUID;
	// no test enumerates the namespace or touches existing native pipes.
	pattern := originPipePrefix + "%d-" + pipeTestUUID(t)
	p := startPipeTestProcess(t, pipeFixtureConfig{Mode: "main", Path: pattern, Stop: mainStop.name, ChildStop: serverStop.name}, mainStop)
	// Register before waiting for readiness: even a failed startup stops the child.
	t.Cleanup(func() { serverStop.signal(t) })
	ready := p.message(t, "ready")
	path := fmt.Sprintf(pattern, p.cmd.Process.Pid)
	if ready.Path != path || ready.PID <= 0 || ready.PID == p.cmd.Process.Pid || ready.PID == os.Getpid() {
		t.Fatalf("incorrect grandchild identity: %+v", ready)
	}
	expected, sid := pipeTestIdentity(t)
	// Pin identity before obtaining a termination-capable cleanup handle. Compare
	// creation times so even cleanup cannot target a reused PID generation.
	pinned, err := pinPipeProcess(uint32(ready.PID), expected, sid)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(pinned.handle)
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(ready.PID))
	if err != nil {
		t.Fatal(err)
	}
	var created, exit, kernel, user windows.Filetime
	err = windows.GetProcessTimes(h, &created, &exit, &kernel, &user)
	if err != nil || created != pinned.created {
		_ = windows.CloseHandle(h)
		t.Fatalf("grandchild cleanup generation mismatch: %v", err)
	}
	t.Cleanup(func() {
		defer windows.CloseHandle(h)
		serverStop.signal(t)
		state, err := windows.WaitForSingleObject(h, 3000)
		if err != nil || state != windows.WAIT_OBJECT_0 {
			t.Errorf("fixture grandchild did not exit: state=%d err=%v", state, err)
			_ = windows.TerminateProcess(h, 2) // Retained handle to our fixture generation only.
			if state, err := windows.WaitForSingleObject(h, 5000); err != nil || state != windows.WAIT_OBJECT_0 {
				t.Errorf("could not reap fixture grandchild: state=%d err=%v", state, err)
			}
		}
	})
	raw, before := dialPipeTest(t, path)
	verified, err := verifyOriginPipe(raw, path, expected, sid, before)
	if err != nil {
		t.Fatal(err)
	}
	ownVerifiedPipe(t, verified)
	mainStop.signal(t)
	select {
	case <-p.waited:
	case <-time.After(3 * time.Second):
		t.Fatal("fixture main did not exit")
	}
	requirePipeMonitorClosed(t, verified)
	if state, err := windows.WaitForSingleObject(h, 0); err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("server must remain alive after main exits: state=%d err=%v", state, err)
	}
}
