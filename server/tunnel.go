package server

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Built-in public access (Welle 40): salt.md manages a cloudflared process
// itself so exposing an instance is a product feature, not an ops exercise.
// Two modes:
//   - "quick": an account-less TryCloudflare tunnel with a throwaway URL —
//     one click, for testing/sharing.
//   - "token": a named Cloudflare Tunnel (user pastes the dashboard token
//     once); supervised with restarts and re-started on boot.
//
// cloudflared is resolved from PATH, then dataDir/bin, and only as a last
// resort downloaded from the official GitHub release (admin-triggered, HTTPS,
// fixed URL pattern).

type tunnelState struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	mode     string // "quick" | "token" | ""
	status   string // "off" | "starting" | "running" | "error"
	url      string // quick-tunnel URL once known
	lastErr  string
	stopping bool
	gen      int // incremented on every stop to cancel stale supervisors
}

// SetAddr tells the server its own listen address so the tunnel knows where
// to point. Called from main before Start.
func (s *Server) SetAddr(addr string) { s.addr = addr }

// localURL converts the listen address into a loopback origin for cloudflared.
func (s *Server) localURL() string {
	addr := s.addr
	if addr == "" {
		addr = ":8420"
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	host, port, ok := strings.Cut(addr, ":")
	if ok && (host == "0.0.0.0" || host == "") {
		return "http://127.0.0.1:" + port
	}
	return "http://" + addr
}

// ---- binary resolution ----

func (s *Server) cloudflaredPath() (string, bool) {
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, true
	}
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(s.dataDir, "bin", name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return p, true
	}
	return p, false
}

// cloudflaredAsset maps GOOS/GOARCH to the official release asset name.
func cloudflaredAsset() (asset string, isTgz bool, err error) {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64", "arm64", "386", "arm":
			return "cloudflared-linux-" + runtime.GOARCH, false, nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64", "arm64":
			return "cloudflared-darwin-" + runtime.GOARCH + ".tgz", true, nil
		}
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "cloudflared-windows-amd64.exe", false, nil
		}
	}
	return "", false, fmt.Errorf("no cloudflared build for %s/%s — install cloudflared manually", runtime.GOOS, runtime.GOARCH)
}

// downloadCloudflared fetches the official release binary into dataDir/bin.
func (s *Server) downloadCloudflared(dest string) error {
	asset, isTgz, err := cloudflaredAsset()
	if err != nil {
		return err
	}
	url := "https://github.com/cloudflare/cloudflared/releases/latest/download/" + asset
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}
	body := io.LimitReader(resp.Body, 200<<20)

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	defer os.Remove(tmp)

	if isTgz {
		gz, err := gzip.NewReader(body)
		if err != nil {
			return err
		}
		tr := tar.NewReader(gz)
		found := false
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			if filepath.Base(hdr.Name) == "cloudflared" && hdr.Typeflag == tar.TypeReg {
				f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
				if err != nil {
					return err
				}
				if _, err := io.Copy(f, io.LimitReader(tr, 200<<20)); err != nil {
					f.Close()
					return err
				}
				f.Close()
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("cloudflared not found in the archive")
		}
	} else {
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, body); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return os.Rename(tmp, dest)
}

// ---- process management ----

var tryCloudflareRe = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

// StartTunnel launches cloudflared in the given mode. Token mode persists the
// desired state so the tunnel comes back after a server restart.
func (s *Server) StartTunnel(mode, token string) error {
	t := &s.tunnel
	t.mu.Lock()
	if t.status == "starting" || t.status == "running" {
		t.mu.Unlock()
		return fmt.Errorf("tunnel is already running")
	}
	t.gen++
	gen := t.gen
	t.mode = mode
	t.status = "starting"
	t.url = ""
	t.lastErr = ""
	t.stopping = false
	t.mu.Unlock()

	fail := func(err error) error {
		t.mu.Lock()
		t.status = "error"
		t.lastErr = err.Error()
		t.mu.Unlock()
		return err
	}

	if mode == "token" {
		if token == "" {
			token = s.setting("tunnel_token", "")
		}
		if token == "" {
			return fail(fmt.Errorf("no tunnel token stored"))
		}
		s.setSetting("tunnel_token", token)
	}

	bin, ok := s.cloudflaredPath()
	if !ok {
		if err := s.downloadCloudflared(bin); err != nil {
			return fail(err)
		}
	}

	var args []string
	switch mode {
	case "quick":
		args = []string{"tunnel", "--no-autoupdate", "--url", s.localURL()}
	case "token":
		// --grace-period: otherwise cloudflared drains for up to 30s after
		// SIGTERM. A restart must not take that long (systemd gives us 20s
		// altogether).
		args = []string{"tunnel", "--no-autoupdate", "--grace-period", "2s", "run", "--token", token}
	default:
		return fail(fmt.Errorf("unknown mode"))
	}

	cmd := exec.Command(bin, args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(err)
	}
	cmd.Stdout = cmd.Stderr // cloudflared logs to stderr; merge just in case
	if err := cmd.Start(); err != nil {
		return fail(fmt.Errorf("cloudflared start: %w", err))
	}

	t.mu.Lock()
	t.cmd = cmd
	t.mu.Unlock()

	// Parse the log stream: quick-tunnel URL + connection registrations.
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64*1024), 512*1024)
		for sc.Scan() {
			line := sc.Text()
			if u := tryCloudflareRe.FindString(line); u != "" {
				t.mu.Lock()
				t.url = u
				t.status = "running"
				t.mu.Unlock()
			}
			if strings.Contains(line, "Registered tunnel connection") {
				t.mu.Lock()
				first := t.status != "running"
				t.status = "running"
				t.mu.Unlock()
				// Without this line the journal never showed WHETHER and WHEN
				// the tunnel came back up after a restart.
				if first {
					log.Printf("tunnel: connected (%s)", mode)
				}
				// Behind Cloudflare the proxy headers are trustworthy — flip it
				// on so rate limiting & audit see real client IPs.
				s.setSetting("trust_proxy", "1")
			}
			if strings.Contains(line, "Unauthorized") || strings.Contains(line, "invalid token") || strings.Contains(line, "failed to parse token") {
				t.mu.Lock()
				t.lastErr = "Token rejected (Cloudflare refused the connection)"
				t.mu.Unlock()
				log.Printf("tunnel: %s", line)
			}
		}
	}()

	// Supervisor: token tunnels restart with backoff; quick tunnels just end.
	go func() {
		err := cmd.Wait()
		t.mu.Lock()
		stopping := t.stopping
		stale := gen != t.gen
		t.cmd = nil
		// An admin-initiated stop bumps gen too — check stopping FIRST so the
		// supervisor still records the clean "off" state for its own exit.
		if stopping {
			t.status = "off"
			t.mode = ""
			t.url = ""
			t.mu.Unlock()
			return
		}
		if stale {
			t.mu.Unlock()
			return
		}
		t.status = "error"
		if t.lastErr == "" {
			if err != nil {
				t.lastErr = "cloudflared beendet: " + err.Error()
			} else {
				t.lastErr = "cloudflared exited on its own"
			}
		}
		mode := t.mode
		lastErr := t.lastErr
		t.mu.Unlock()
		log.Printf("tunnel: cloudflared exited (%s) — %s", mode, lastErr)
		if mode == "token" && s.boolSetting("tunnel_autostart") {
			log.Print("tunnel: retrying in 5s")
			time.Sleep(5 * time.Second)
			t.mu.Lock()
			stale := gen != t.gen
			t.mu.Unlock()
			// Re-check autostart after the backoff — the admin may have hit
			// Stop while we were sleeping.
			if !stale && s.boolSetting("tunnel_autostart") {
				s.StartTunnel("token", "")
			}
		}
	}()

	if mode == "token" {
		s.setSetting("tunnel_autostart", "1")
	}
	return nil
}

// StopTunnel terminates the managed process and disables autostart.
func (s *Server) StopTunnel() {
	t := &s.tunnel
	t.mu.Lock()
	t.stopping = true
	t.gen++
	cmd := t.cmd
	if cmd == nil {
		t.status = "off"
		t.mode = ""
		t.url = ""
	}
	t.mu.Unlock()
	s.setSetting("tunnel_autostart", "")
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
		// The wait-goroutine sets status=off (stopping=true).
	}
}

// ShutdownTunnel ends the managed cloudflared when the process itself exits.
//
// Deliberately NOT StopTunnel: that one is the admin's explicit "off" and
// therefore clears `tunnel_autostart` — calling it here would silently disable
// public access on every restart. The other half matters just as much: a
// SIGKILLed cloudflared never tells Cloudflare's edge that it is going away,
// so the dead connection stays registered and the next process needs minutes
// before the domain answers again. SIGTERM lets it deregister first.
// Split in two so the deregistration runs ALONGSIDE the HTTP drain instead of
// after it: systemd grants only 20s to stop, and the drain alone takes up to 15.
func (s *Server) SignalTunnelStop() {
	t := &s.tunnel
	t.mu.Lock()
	cmd := t.cmd
	if cmd == nil || cmd.Process == nil {
		t.mu.Unlock()
		return
	}
	if t.stopping {
		t.mu.Unlock() // main.go already signalled; Close() calls again
		return
	}
	t.stopping = true // keeps the supervisor from restarting it
	t.gen++
	t.mu.Unlock()
	log.Print("tunnel: stopping cloudflared…")
	_ = cmd.Process.Signal(syscall.SIGTERM)
}

// AwaitTunnelStop waits for the end signalled above and kills hard if
// cloudflared hangs.
func (s *Server) AwaitTunnelStop(max time.Duration) {
	t := &s.tunnel
	t.mu.Lock()
	cmd := t.cmd
	t.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	// The supervisor owns cmd.Wait() — so wait for the state IT sets rather
	// than waiting a second time (which would be an error).
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		t.mu.Lock()
		gone := t.cmd == nil
		t.mu.Unlock()
		if gone {
			log.Print("tunnel: cloudflared stopped")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Print("tunnel: cloudflared ignored SIGTERM — killed")
	_ = cmd.Process.Kill()
}

// waitForOrigin blocks until the own HTTP listener accepts connections.
func (s *Server) waitForOrigin(max time.Duration) bool {
	addr := s.addr
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			c.Close()
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// AutostartTunnel resumes a persistent token tunnel after a restart.
func (s *Server) AutostartTunnel() {
	if !s.boolSetting("tunnel_autostart") || s.setting("tunnel_token", "") == "" {
		return
	}
	go func() {
		// cloudflared has to find the origin already answering: it connects to
		// the edge immediately and a refused origin means the domain serves 502
		// until a health check recovers. main() only starts the listener a few
		// lines AFTER this call, so wait for the port instead of guessing with
		// a fixed sleep (which is what used to make restarts drop the domain).
		if !s.waitForOrigin(30 * time.Second) {
			log.Printf("tunnel: origin %s never came up — starting cloudflared anyway", s.addr)
		}
		log.Print("tunnel: autostart (stored token)")
		if err := s.StartTunnel("token", ""); err != nil {
			log.Printf("tunnel autostart: %v", err)
		}
	}()
}

// PublicHTTPSConfig exposes the built-in HTTPS settings to main().
func (s *Server) PublicHTTPSConfig() (domain string, enabled bool) {
	return s.setting("https_domain", ""), s.boolSetting("https_enabled")
}

// ---- admin endpoints ----

func (s *Server) handlePublicAccess(w http.ResponseWriter, r *http.Request) {
	if !requestUser(r).IsAdmin {
		httpError(w, 403, "admin only")
		return
	}
	t := &s.tunnel
	t.mu.Lock()
	status, mode, url, lastErr := t.status, t.mode, t.url, t.lastErr
	t.mu.Unlock()
	if status == "" {
		status = "off"
	}
	_, found := s.cloudflaredPath()
	domain, httpsEnabled := s.PublicHTTPSConfig()
	writeJSON(w, map[string]any{
		"status":          status,
		"mode":            mode,
		"url":             url,
		"lastError":       lastErr,
		"tokenSet":        s.setting("tunnel_token", "") != "",
		"autostart":       s.boolSetting("tunnel_autostart"),
		"cloudflaredHere": found,
		"httpsDomain":     domain,
		"httpsEnabled":    httpsEnabled,
		"localUrl":        s.localURL(),
	})
}

func (s *Server) handleTunnelAction(w http.ResponseWriter, r *http.Request) {
	if !requestUser(r).IsAdmin {
		httpError(w, 403, "admin only")
		return
	}
	var body struct {
		Action string `json:"action"` // start-quick | start-token | stop
		Token  string `json:"token"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	switch body.Action {
	case "start-quick":
		if err := s.StartTunnel("quick", ""); err != nil {
			httpError(w, 400, err.Error())
			return
		}
	case "start-token":
		if err := s.StartTunnel("token", strings.TrimSpace(body.Token)); err != nil {
			httpError(w, 400, err.Error())
			return
		}
	case "stop":
		s.StopTunnel()
	default:
		httpError(w, 400, "unknown action")
		return
	}
	s.handlePublicAccess(w, r)
}
