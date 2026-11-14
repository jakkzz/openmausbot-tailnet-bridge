//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

const originPipePrefix = `\\.\pipe\openmausbot-companion-origin-`

// The native allocator uses randomUUID(), not arbitrary 36-character strings.
var originPipePattern = regexp.MustCompile(`(?i)^openmausbot-companion-origin-([1-9][0-9]*)-[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type pinnedProcess struct {
	handle  windows.Handle
	created windows.Filetime
}

func newPipeDialer(expectedExe string) (func(context.Context, string, string) (net.Conn, error), error) {
	// Do not accept relative, drive-relative, or network image paths.
	if !filepath.IsAbs(expectedExe) || len(filepath.VolumeName(expectedExe)) != 2 {
		return nil, errors.New("expected executable must be an absolute local drive path")
	}
	expected, err := canonicalPipeImage(expectedExe)
	if err != nil {
		return nil, fmt.Errorf("expected executable: %w", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	sid := user.User.Sid.String()
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		paths, err := enumerateOriginPipes()
		if err != nil {
			return nil, err
		}
		var selected *verifiedPipeConn
		keep := false
		defer func() {
			if !keep && selected != nil {
				_ = selected.Close()
			}
		}()
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			// A busy, vanished, inaccessible, or otherwise unverifiable candidate is
			// not silently ignored. No probe ever sends application data.
			probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			var beforeDial windows.Filetime
			windows.GetSystemTimeAsFileTime(&beforeDial)
			conn, err := winio.DialPipeContext(probeCtx, path)
			cancel()
			if err != nil {
				return nil, fmt.Errorf("private pipe probe: %w", err)
			}
			verified, err := verifyOriginPipe(conn, path, expected, sid, beforeDial)
			if err != nil {
				_ = conn.Close()
				return nil, err
			}
			if selected != nil {
				_ = verified.Close()
				return nil, errors.New("ambiguous private pipe candidates")
			}
			selected = verified
		}
		if selected == nil {
			return nil, errors.New("no verified private origin pipe")
		}
		// Enumeration is not atomic. Reject visible namespace changes during probes.
		again, err := enumerateOriginPipes()
		if err != nil {
			return nil, err
		}
		if strings.Join(paths, "\x00") != strings.Join(again, "\x00") {
			return nil, errors.New("private pipe namespace changed during verification")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := selected.checkAlive(); err != nil {
			return nil, err
		}
		keep = true
		return selected, nil
	}, nil
}

func enumerateOriginPipes() ([]string, error) {
	pattern, err := windows.UTF16PtrFromString(originPipePrefix + "*")
	if err != nil {
		return nil, err
	}
	var data windows.Win32finddata
	// x/sys binds these to FindFirstFileW / FindNextFileW.
	h, err := windows.FindFirstFile(pattern, &data)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer windows.FindClose(h)
	var paths []string
	for {
		name := windows.UTF16ToString(data.FileName[:])
		if originPipePattern.MatchString(name) {
			paths = append(paths, `\\.\pipe\`+name)
		}
		err = windows.FindNextFile(h, &data)
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func canonicalPipeImage(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, 32768)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	if n == 0 || n >= uint32(len(buf)) {
		return "", errors.New("invalid canonical image path length")
	}
	return windows.UTF16ToString(buf[:n]), nil
}

func pinPipeProcess(pid uint32, expected, sid string) (p pinnedProcess, err error) {
	p.handle, err = windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return p, err
	}
	defer func() {
		if err != nil {
			windows.CloseHandle(p.handle)
			p.handle = 0
		}
	}()
	token := windows.Token(0)
	if err = windows.OpenProcessToken(p.handle, windows.TOKEN_QUERY, &token); err != nil {
		return p, err
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return p, err
	}
	if user.User.Sid.String() != sid {
		return p, errors.New("private pipe process belongs to another user")
	}
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if err = windows.QueryFullProcessImageName(p.handle, 0, &buf[0], &n); err != nil {
		return p, err
	}
	image, err := canonicalPipeImage(windows.UTF16ToString(buf[:n]))
	if err != nil {
		return p, err
	}
	if !strings.EqualFold(image, expected) {
		return p, errors.New("private pipe process image mismatch")
	}
	var exit, kernel, userTime windows.Filetime
	if err = windows.GetProcessTimes(p.handle, &p.created, &exit, &kernel, &userTime); err != nil {
		return p, err
	}
	err = p.alive()
	return p, err
}

func (p pinnedProcess) alive() error {
	state, err := windows.WaitForSingleObject(p.handle, 0)
	if err != nil {
		return err
	}
	if state != uint32(windows.WAIT_TIMEOUT) {
		return errors.New("private origin process exited")
	}
	var created, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(p.handle, &created, &exit, &kernel, &user); err != nil {
		return err
	}
	if created != p.created {
		return errors.New("private origin process identity changed")
	}
	return nil
}

// go-winio v0.6.2 (and v0.6.4) promotes Fd(), but has no SyscallConn.
// This adapter is used ONLY before publication: there is no concurrent Close,
// read or write, so the handle remains valid throughout RawConn.Control.
// Prefer the SDK's own SyscallConn if a later version supplies one.
type unpublishedPipeRawConn struct {
	conn net.Conn
	fd   uintptr
}

func (r unpublishedPipeRawConn) Control(f func(uintptr)) error {
	f(r.fd)
	runtime.KeepAlive(r.conn)
	return nil
}
func (r unpublishedPipeRawConn) Read(func(uintptr) bool) error {
	return errors.New("control-only pipe handle")
}
func (r unpublishedPipeRawConn) Write(func(uintptr) bool) error {
	return errors.New("control-only pipe handle")
}

type unpublishedPipeConn struct{ net.Conn }

func (p unpublishedPipeConn) SyscallConn() (syscall.RawConn, error) {
	if c, ok := p.Conn.(syscall.Conn); ok {
		return c.SyscallConn()
	}
	c, ok := p.Conn.(interface{ Fd() uintptr })
	if !ok {
		return nil, errors.New("pipe SDK exposes no native handle")
	}
	return unpublishedPipeRawConn{p.Conn, c.Fd()}, nil
}

func actualPipeServerPID(conn net.Conn) (uint32, error) {
	raw, err := (unpublishedPipeConn{conn}).SyscallConn()
	if err != nil {
		return 0, err
	}
	var pid uint32
	var queryErr error
	err = raw.Control(func(fd uintptr) { queryErr = windows.GetNamedPipeServerProcessId(windows.Handle(fd), &pid) })
	if err != nil {
		return 0, err
	}
	if queryErr != nil {
		return 0, queryErr
	}
	if pid == 0 {
		return 0, errors.New("private pipe has no server PID")
	}
	return pid, nil
}

func verifyOriginPipe(conn net.Conn, path, expected, sid string, beforeDial windows.Filetime) (_ *verifiedPipeConn, err error) {
	match := originPipePattern.FindStringSubmatch(strings.TrimPrefix(path, `\\.\pipe\`))
	if match == nil {
		return nil, errors.New("invalid private pipe path")
	}
	mainPID, err := strconv.ParseUint(match[1], 10, 32)
	if err != nil {
		return nil, err
	}
	serverPID, err := actualPipeServerPID(conn)
	if err != nil {
		return nil, err
	}
	if serverPID == uint32(mainPID) {
		return nil, errors.New("private sidecar is not a child process")
	}
	server, err := pinPipeProcess(serverPID, expected, sid)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			windows.CloseHandle(server.handle)
		}
	}()
	main, err := pinPipeProcess(uint32(mainPID), expected, sid)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			windows.CloseHandle(main.handle)
		}
	}()
	// Query parent directly from the pinned server, not from cached metadata.
	var info windows.PROCESS_BASIC_INFORMATION
	if err = windows.NtQueryInformationProcess(server.handle, windows.ProcessBasicInformation,
		unsafe.Pointer(&info), uint32(unsafe.Sizeof(info)), nil); err != nil {
		return nil, err
	}
	if info.UniqueProcessId != uintptr(serverPID) || info.InheritedFromUniqueProcessId != uintptr(mainPID) {
		return nil, errors.New("private sidecar parent mismatch")
	}
	// A reused parent PID cannot stand in for the actual, older parent.
	stamp := func(t windows.Filetime) uint64 { return uint64(t.HighDateTime)<<32 | uint64(t.LowDateTime) }
	if stamp(main.created) >= stamp(server.created) || stamp(server.created) >= stamp(beforeDial) {
		return nil, errors.New("private origin process generation mismatch")
	}
	again, err := actualPipeServerPID(conn)
	if err != nil {
		return nil, err
	}
	if again != serverPID {
		return nil, errors.New("private pipe server changed")
	}
	if err = server.alive(); err != nil {
		return nil, err
	}
	if err = main.alive(); err != nil {
		return nil, err
	}
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, err
	}
	c := &verifiedPipeConn{Conn: conn, server: server, main: main, stop: stop, done: make(chan struct{})}
	go c.monitor()
	return c, nil
}

type verifiedPipeConn struct {
	net.Conn
	server, main pinnedProcess
	stop         windows.Handle
	mu           sync.Mutex // protects stop handle from SetEvent / CloseHandle races
	closed       bool
	closeErr     error
	done         chan struct{}
}

func (c *verifiedPipeConn) checkAlive() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	if err := c.server.alive(); err != nil {
		return err
	}
	return c.main.alive()
}

// Guard writes as well as monitoring: scheduling delay after an exit must not
// make a previously verified, now-dead generation acceptable for a new write.
func (c *verifiedPipeConn) Write(b []byte) (int, error) {
	if err := c.checkAlive(); err != nil {
		_ = c.Close()
		return 0, err
	}
	return c.Conn.Write(b)
}

func (c *verifiedPipeConn) monitor() {
	// The event wakes normal Close immediately. A bounded wait also guarantees
	// cleanup if SetEvent unexpectedly fails; any failed wait fails closed.
	for {
		result, err := windows.WaitForMultipleObjects([]windows.Handle{c.server.handle, c.main.handle, c.stop}, false, 1000)
		if err != nil || result != uint32(windows.WAIT_TIMEOUT) {
			break
		}
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if closed {
			break
		}
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	// Do not hold mu across Close: cancelling in-flight pipe I/O can block.
	err := c.Conn.Close()
	c.mu.Lock()
	c.closeErr = err
	windows.CloseHandle(c.server.handle)
	windows.CloseHandle(c.main.handle)
	windows.CloseHandle(c.stop)
	c.stop = 0
	c.mu.Unlock()
	close(c.done)
}

func (c *verifiedPipeConn) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		// Our unnamed event is valid until the monitor closes it under mu.
		if err := windows.SetEvent(c.stop); err != nil {
			c.mu.Unlock()
			// Fail closed even on an unexpected event failure.
			_ = c.Conn.Close()
			<-c.done
			return err
		}
	}
	c.mu.Unlock()
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}
