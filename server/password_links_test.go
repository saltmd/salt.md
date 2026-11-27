package server

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Password links end to end: issued through the real router, delivered to a
// local SMTP sink, opened and used like a browser would.

type mailSink struct {
	messages chan string
}

// startMailSink runs a minimal SMTP server and points the instance at it.
func startMailSink(t *testing.T, s *Server) *mailSink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	sink := &mailSink{messages: make(chan string, 10)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go sink.serve(conn)
		}
	}()
	s.setSetting("smtp_host", "127.0.0.1")
	s.setSetting("smtp_port", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))
	s.setSetting("smtp_from", "salt@example.com")
	s.setSetting(settingPasswordResetSelf, "1")
	return sink
}

func (m *mailSink) serve(conn net.Conn) {
	defer conn.Close()
	rd := bufio.NewReader(conn)
	reply := func(line string) { conn.Write([]byte(line + "\r\n")) }
	reply("220 sink")
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(cmd, "DATA"):
			reply("354 go on")
			var msg strings.Builder
			for {
				l, err := rd.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				msg.WriteString(l)
			}
			m.messages <- msg.String()
			reply("250 queued")
		case strings.HasPrefix(cmd, "QUIT"):
			reply("221 bye")
			return
		default:
			reply("250 ok")
		}
	}
}

func (m *mailSink) next(t *testing.T) string {
	t.Helper()
	select {
	case msg := <-m.messages:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("no mail arrived")
		return ""
	}
}

func (m *mailSink) none(t *testing.T) {
	t.Helper()
	select {
	case msg := <-m.messages:
		t.Fatalf("unexpected mail: %s", msg)
	case <-time.After(300 * time.Millisecond):
	}
}

var linkTokenPattern = regexp.MustCompile(`/set-password/([a-f0-9]{64})`)

func linkToken(t *testing.T, text string) string {
	t.Helper()
	m := linkTokenPattern.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no password link in: %s", text)
	}
	return m[1]
}

// instanceAdmin creates a non-owner instance admin and signs them in.
func instanceAdmin(t *testing.T, s *Server, email string) string {
	t.Helper()
	id := newID()
	if _, err := s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at)
		VALUES (?, ?, 'Admin', '#2f7d4f', ?, 1, ?)`, id, email, hashPassword("passwort12345"), now()); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	s.addOrgMember(id, true)
	tok, err := s.createSession(id)
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	return sessionCookie + "=" + tok
}

func loginCode(t *testing.T, s *Server, email, password string) int {
	t.Helper()
	return requestAs(t, s, "", "POST", "/api/login", `{"email":"`+email+`","password":"`+password+`"}`).Code
}

func decodeBody[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return v
}

type createdUserResponse struct {
	ID     string             `json:"id"`
	Invite passwordLinkResult `json:"invite"`
}

func TestInviteEmailSetsPasswordOnce(t *testing.T) {
	s := testServer(t)
	sink := startMailSink(t, s)
	_, owner := signedIn(t, s, "owner@example.com")

	rec := requestAs(t, s, owner, "POST", "/api/users", `{"name":"Nora","email":"nora@example.com","sendInvite":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	created := decodeBody[createdUserResponse](t, rec.Body.Bytes())
	if !created.Invite.Emailed || created.Invite.Link != "" || created.Invite.Purpose != passwordLinkInvite {
		t.Fatalf("invite result: %+v", created.Invite)
	}
	mail := sink.next(t)
	if !strings.Contains(mail, "To: nora@example.com") || !strings.Contains(mail, "7 days") {
		t.Fatalf("invite mail: %s", mail)
	}
	token := linkToken(t, mail)

	if code := loginCode(t, s, "nora@example.com", ""); code != http.StatusUnauthorized {
		t.Fatalf("passwordless account signed in: %d", code)
	}
	info := requestAs(t, s, "", "GET", "/api/password-links/"+token, "")
	if info.Code != http.StatusOK || !strings.Contains(info.Body.String(), `"purpose":"invite"`) {
		t.Fatalf("info: %d %s", info.Code, info.Body.String())
	}
	if rec := requestAs(t, s, "", "POST", "/api/password-links/"+token, `{"password":"short"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("short password accepted: %d", rec.Code)
	}
	if rec := requestAs(t, s, "", "POST", "/api/password-links/"+token, `{"password":"nora-chosen-pw"}`); rec.Code != http.StatusOK {
		t.Fatalf("use: %d %s", rec.Code, rec.Body.String())
	}
	if code := loginCode(t, s, "nora@example.com", "nora-chosen-pw"); code != http.StatusOK {
		t.Fatalf("login after invite: %d", code)
	}
	if rec := requestAs(t, s, "", "POST", "/api/password-links/"+token, `{"password":"another-pw-123"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("reused link: %d", rec.Code)
	}
	var detail string
	s.db.QueryRow(`SELECT group_concat(detail, '|') FROM audit_log`).Scan(&detail)
	if strings.Contains(detail, token) || !strings.Contains(detail, "Nora (emailed)") {
		t.Fatalf("audit: %s", detail)
	}
}

func TestResetEmailReplacesPassword(t *testing.T) {
	s := testServer(t)
	sink := startMailSink(t, s)
	signedIn(t, s, "owner@example.com")
	uid, _ := signedIn(t, s, "max@example.com")
	admin := instanceAdmin(t, s, "admin@example.com")

	send := func() string {
		rec := requestAs(t, s, admin, "POST", "/api/users/"+uid+"/password-link", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("send: %d %s", rec.Code, rec.Body.String())
		}
		res := decodeBody[passwordLinkResult](t, rec.Body.Bytes())
		if !res.Emailed || res.Link != "" || res.Purpose != passwordLinkReset {
			t.Fatalf("reset result: %+v", res)
		}
		return linkToken(t, sink.next(t))
	}
	first := send()
	second := send()
	if rec := requestAs(t, s, "", "GET", "/api/password-links/"+first, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("older link still valid: %d", rec.Code)
	}
	if code := loginCode(t, s, "max@example.com", "passwort12345"); code != http.StatusOK {
		t.Fatalf("old password stopped working before the link was used: %d", code)
	}
	if rec := requestAs(t, s, "", "POST", "/api/password-links/"+second, `{"password":"max-new-password"}`); rec.Code != http.StatusOK {
		t.Fatalf("use: %d %s", rec.Code, rec.Body.String())
	}
	if code := loginCode(t, s, "max@example.com", "passwort12345"); code != http.StatusUnauthorized {
		t.Fatalf("old password still works: %d", code)
	}
	if code := loginCode(t, s, "max@example.com", "max-new-password"); code != http.StatusOK {
		t.Fatalf("new password: %d", code)
	}
	var sessions int
	s.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, uid).Scan(&sessions)
	if sessions != 1 {
		t.Fatalf("sessions before the reset survived: %d", sessions)
	}
}

func TestExpiredPasswordLinkRejected(t *testing.T) {
	s := testServer(t)
	signedIn(t, s, "owner@example.com")
	uid, _ := signedIn(t, s, "max@example.com")
	token, _, err := s.issuePasswordLink(uid, passwordLinkReset, uid)
	if err != nil {
		t.Fatal(err)
	}
	s.db.Exec(`UPDATE password_links SET expires_at = ? WHERE user_id = ?`,
		time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano), uid)
	if rec := requestAs(t, s, "", "POST", "/api/password-links/"+token, `{"password":"max-new-password"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("expired link used: %d", rec.Code)
	}
	if code := loginCode(t, s, "max@example.com", "passwort12345"); code != http.StatusOK {
		t.Fatalf("password changed by an expired link: %d", code)
	}
}

func TestPasswordLinkWithoutMail(t *testing.T) {
	s := testServer(t)
	_, owner := signedIn(t, s, "owner@example.com")
	uid, _ := signedIn(t, s, "max@example.com")
	admin := instanceAdmin(t, s, "admin@example.com")

	// An admin can hand on an invitation, but not a reset of somebody's password.
	rec := requestAs(t, s, admin, "POST", "/api/users", `{"name":"Nora","email":"nora@example.com","sendInvite":true}`)
	created := decodeBody[createdUserResponse](t, rec.Body.Bytes())
	if created.Invite.Emailed || !strings.Contains(created.Invite.Link, "/set-password/") {
		t.Fatalf("invite without mail: %+v", created.Invite)
	}
	rec = requestAs(t, s, admin, "POST", "/api/users/"+uid+"/password-link", "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "password_reset_needs_mail") {
		t.Fatalf("admin reset without mail: %d %s", rec.Code, rec.Body.String())
	}
	var left int
	s.db.QueryRow(`SELECT COUNT(*) FROM password_links WHERE user_id = ?`, uid).Scan(&left)
	if left != 0 {
		t.Fatal("an undelivered reset link was kept")
	}
	rec = requestAs(t, s, owner, "POST", "/api/users/"+uid+"/password-link", "")
	res := decodeBody[passwordLinkResult](t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || res.Emailed || res.Link == "" {
		t.Fatalf("owner reset without mail: %d %s", rec.Code, rec.Body.String())
	}
	s.setSetting(settingPasswordResetSelf, "1")
	if rec := requestAs(t, s, "", "POST", "/api/password-reset", `{"email":"max@example.com"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("self-service without mail: %d", rec.Code)
	}
}

func TestPasswordLinkNeedsAdminSession(t *testing.T) {
	s := testServer(t)
	signedIn(t, s, "owner@example.com")
	uid, member := signedIn(t, s, "max@example.com")
	if rec := requestAs(t, s, member, "POST", "/api/users/"+uid+"/password-link", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member sent a link: %d", rec.Code)
	}
	if rec := requestAs(t, s, "", "POST", "/api/users/"+uid+"/password-link", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous sent a link: %d", rec.Code)
	}
}

func TestForgotPasswordGivesNothingAway(t *testing.T) {
	s := testServer(t)
	sink := startMailSink(t, s)
	signedIn(t, s, "owner@example.com")
	signedIn(t, s, "max@example.com")

	unknown := requestAs(t, s, "", "POST", "/api/password-reset", `{"email":"nobody@example.com"}`)
	known := requestAs(t, s, "", "POST", "/api/password-reset", `{"email":"max@example.com"}`)
	if unknown.Code != http.StatusOK || known.Code != http.StatusOK || unknown.Body.String() != known.Body.String() {
		t.Fatalf("answers differ: %d %s / %d %s", unknown.Code, unknown.Body.String(), known.Code, known.Body.String())
	}
	token := linkToken(t, sink.next(t))
	sink.none(t)

	// Within the cooldown a second request sends nothing and keeps the link.
	requestAs(t, s, "", "POST", "/api/password-reset", `{"email":"max@example.com"}`)
	sink.none(t)
	if rec := requestAs(t, s, "", "POST", "/api/password-links/"+token, `{"password":"max-new-password"}`); rec.Code != http.StatusOK {
		t.Fatalf("use: %d %s", rec.Code, rec.Body.String())
	}
	if code := loginCode(t, s, "max@example.com", "max-new-password"); code != http.StatusOK {
		t.Fatalf("login after reset: %d", code)
	}

	// The per-address budget runs out.
	limited := false
	for i := 0; i < 10; i++ {
		if requestAs(t, s, "", "POST", "/api/password-reset", `{"email":"nobody@example.com"}`).Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("self-service reset is not rate limited")
	}
}

func TestForgotPasswordOffByDefault(t *testing.T) {
	s := testServer(t)
	sink := startMailSink(t, s)
	s.setSetting(settingPasswordResetSelf, "")
	signedIn(t, s, "owner@example.com")
	signedIn(t, s, "max@example.com")
	if rec := requestAs(t, s, "", "GET", "/api/signup-policy", ""); !strings.Contains(rec.Body.String(), `"passwordReset":false`) {
		t.Fatalf("policy: %s", rec.Body.String())
	}
	if rec := requestAs(t, s, "", "POST", "/api/password-reset", `{"email":"max@example.com"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("self-service while off: %d", rec.Code)
	}
	sink.none(t)
}

func TestDisabledAccountGetsNoPasswordLink(t *testing.T) {
	s := testServer(t)
	sink := startMailSink(t, s)
	_, owner := signedIn(t, s, "owner@example.com")
	uid, _ := signedIn(t, s, "max@example.com")
	token, _, _ := s.issuePasswordLink(uid, passwordLinkReset, uid)
	s.db.Exec(`UPDATE users SET disabled = 1 WHERE id = ?`, uid)

	if rec := requestAs(t, s, owner, "POST", "/api/users/"+uid+"/password-link", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("link for a deactivated account: %d", rec.Code)
	}
	requestAs(t, s, "", "POST", "/api/password-reset", `{"email":"max@example.com"}`)
	sink.none(t)
	if rec := requestAs(t, s, "", "POST", "/api/password-links/"+token, `{"password":"max-new-password"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("deactivated account used a link: %d", rec.Code)
	}
}
