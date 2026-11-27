package server

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"strings"
	"time"
)

// Password links: a single-use, expiring link that lets the person behind an
// address choose their own password. Two kinds, decided by the account:
//
//	invite  the account was created without a password and waits for one
//	reset   the account has a password and gets to replace it
//
// Only the SHA-256 of the token is stored. Issuing a link removes every older
// link of the account, using one removes them all, and so does any other
// password change.

const (
	passwordLinkInvite = "invite"
	passwordLinkReset  = "reset"
	passwordLinkPath   = "/set-password/"
	// One self-service request per account in this window; further requests
	// are answered the same way but send nothing.
	passwordLinkCooldown = 2 * time.Minute
	// Admin switch for the self-service request on the sign-in page.
	settingPasswordResetSelf = "password_reset_self"
)

type passwordLinkKind struct {
	ttl      time.Duration
	validFor string
}

var passwordLinkKinds = map[string]passwordLinkKind{
	passwordLinkInvite: {ttl: 7 * 24 * time.Hour, validFor: "7 days"},
	passwordLinkReset:  {ttl: 24 * time.Hour, validFor: "24 hours"},
}

// passwordLinkResult is what an admin gets back after sending a link. Link is
// set only when no email went out and the caller may pass the link on.
type passwordLinkResult struct {
	Purpose   string `json:"purpose"`
	Emailed   bool   `json:"emailed"`
	ExpiresAt string `json:"expiresAt"`
	Link      string `json:"link,omitempty"`
	MailError string `json:"mailError,omitempty"`
}

// invitePending reports whether the account has never had a password.
func (s *Server) invitePending(userID string) bool {
	var hash string
	s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	return hash == ""
}

func (s *Server) passwordLinkPurpose(userID string) string {
	if s.invitePending(userID) {
		return passwordLinkInvite
	}
	return passwordLinkReset
}

// issuePasswordLink replaces the account's links with a fresh one and returns
// the token and its expiry.
func (s *Server) issuePasswordLink(userID, purpose, createdBy string) (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(b)
	expires := time.Now().UTC().Add(passwordLinkKinds[purpose].ttl).Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM password_links WHERE user_id = ?`, userID); err != nil {
		return "", "", err
	}
	if _, err := tx.Exec(`INSERT INTO password_links (token_hash, user_id, purpose, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		tokenHash(token), userID, purpose, createdBy, now(), expires); err != nil {
		return "", "", err
	}
	return token, expires, tx.Commit()
}

func (s *Server) dropPasswordLinks(userID string) {
	s.db.Exec(`DELETE FROM password_links WHERE user_id = ?`, userID)
}

func (s *Server) instanceDisplayName() string {
	return s.setting("instance_name", "salt.md")
}

// passwordLinkMail is the message for a link. English, like the workspace
// invitation: the server keeps no translations.
func (s *Server) passwordLinkMail(purpose string, target *user, link string) (string, string) {
	name := s.instanceDisplayName()
	validity := "The link works once and is valid for " + passwordLinkKinds[purpose].validFor + "."
	if purpose == passwordLinkInvite {
		return "Your account on " + name,
			"An account on " + name + " has been created for you.\n\n" +
				"Open this link to choose your password:\n" + link + "\n\n" +
				validity + "\nYou sign in with this email address: " + target.Email
	}
	return "Reset your password on " + name,
		"A password reset was requested for your account on " + name + " (" + target.Email + ").\n\n" +
			"Open this link to choose a new password:\n" + link + "\n\n" +
			validity + "\nIf you did not ask for this, ignore this message. Your password stays as it is."
}

// sendPasswordLink issues a link for target and emails it. Without delivery the
// link goes back to the caller instead, if they may hold it: always for an
// invitation, for a reset only to the owner (see owner_only_credentials).
func (s *Server) sendPasswordLink(r *http.Request, by, target *user) (passwordLinkResult, error) {
	purpose := s.passwordLinkPurpose(target.ID)
	token, expires, err := s.issuePasswordLink(target.ID, purpose, by.ID)
	if err != nil {
		return passwordLinkResult{}, err
	}
	res := passwordLinkResult{Purpose: purpose, ExpiresAt: expires}
	link := s.publicShareBase(r) + passwordLinkPath + token
	subject, body := s.passwordLinkMail(purpose, target, link)
	mailErr := s.sendMail(target.Email, subject, body)
	switch {
	case mailErr == nil:
		res.Emailed = true
	case purpose == passwordLinkInvite || s.isOwner(by.ID):
		res.Link = link
		res.MailError = mailErr.Error()
	default:
		s.dropPasswordLinks(target.ID)
		return res, coded("password_reset_needs_mail",
			"The reset email could not be sent. Without mail delivery only the owner can be shown the link.", mailErr.Error())
	}
	delivery := "emailed"
	if !res.Emailed {
		delivery = "link shown"
	}
	s.audit("human", by.ID, by.Name, "send_password_link_"+purpose, "", "", target.Name+" ("+delivery+")")
	return res, nil
}

// handleSendPasswordLink: POST /api/users/{id}/password-link.
func (s *Server) handleSendPasswordLink(w http.ResponseWriter, r *http.Request) {
	me := requestUser(r)
	target := s.userByID(r.PathValue("id"))
	if target == nil {
		httpError(w, 404, "user not found")
		return
	}
	if target.Disabled {
		httpErrorCode(w, 400, "account_disabled", "This account has been deactivated. Talk to an admin.")
		return
	}
	res, err := s.sendPasswordLink(r, me, target)
	if err != nil {
		httpErrorFrom(w, 400, err)
		return
	}
	writeJSON(w, res)
}

// handleForgotPassword: POST /api/password-reset. The answer is the same
// whether or not the address has an account, and the mail leaves in the
// background so the response time gives nothing away either.
func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if !s.resetRate.allow(s.clientIP(r)) {
		httpError(w, http.StatusTooManyRequests, "too many attempts, please wait")
		return
	}
	if !s.selfServiceResetOn() {
		httpErrorCode(w, 403, "password_reset_off", "This instance does not send password reset links on request. Ask an admin.")
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	var id string
	var disabled int
	err := s.db.QueryRow(`SELECT id, disabled FROM users WHERE email = ?`,
		strings.ToLower(strings.TrimSpace(body.Email))).Scan(&id, &disabled)
	if err == nil && disabled == 0 && !s.recentPasswordLink(id) {
		s.mailPasswordLinkInBackground(r, s.userByID(id))
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// selfServiceResetOn: an admin switched it on and mail can go out.
func (s *Server) selfServiceResetOn() bool {
	return s.boolSetting(settingPasswordResetSelf) && s.mailConfigured()
}

func (s *Server) recentPasswordLink(userID string) bool {
	var created string
	if s.db.QueryRow(`SELECT created_at FROM password_links WHERE user_id = ? ORDER BY created_at DESC LIMIT 1`, userID).Scan(&created) != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, created)
	return err == nil && time.Since(t) < passwordLinkCooldown
}

func (s *Server) mailPasswordLinkInBackground(r *http.Request, target *user) {
	purpose := s.passwordLinkPurpose(target.ID)
	token, _, err := s.issuePasswordLink(target.ID, purpose, target.ID)
	if err != nil {
		log.Printf("password link for %s: %v", target.ID, err)
		return
	}
	subject, body := s.passwordLinkMail(purpose, target, s.publicShareBase(r)+passwordLinkPath+token)
	s.audit("human", target.ID, target.Name, "request_password_reset", "", "", target.Name)
	go func() {
		if err := s.sendMail(target.Email, subject, body); err != nil {
			log.Printf("password link mail for %s: %v", target.ID, err)
		}
	}()
}

// livePasswordLink resolves a token to its account, or nil when it is unknown
// or expired.
func (s *Server) livePasswordLink(token string) (*user, string) {
	var userID, purpose, expires string
	if s.db.QueryRow(`SELECT user_id, purpose, expires_at FROM password_links WHERE token_hash = ?`,
		tokenHash(token)).Scan(&userID, &purpose, &expires) != nil {
		return nil, ""
	}
	exp, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || time.Now().After(exp) {
		return nil, ""
	}
	u := s.userByID(userID)
	if u == nil || u.Disabled {
		return nil, ""
	}
	return u, purpose
}

func passwordLinkInvalid(w http.ResponseWriter) {
	httpErrorCode(w, 404, "password_link_invalid", "This link is not valid any more. It may have been used already or expired.")
}

// handlePasswordLinkInfo: GET /api/password-links/{token}.
func (s *Server) handlePasswordLinkInfo(w http.ResponseWriter, r *http.Request) {
	u, purpose := s.livePasswordLink(r.PathValue("token"))
	if u == nil {
		passwordLinkInvalid(w)
		return
	}
	writeJSON(w, map[string]string{"email": u.Email, "name": u.Name, "purpose": purpose})
}

// handleUsePasswordLink: POST /api/password-links/{token}. Sets the password,
// ends every session and token of the account, and leaves signing in to the
// sign-in screen, so a second factor still applies.
func (s *Server) handleUsePasswordLink(w http.ResponseWriter, r *http.Request) {
	if !s.loginRate.allow(s.clientIP(r)) {
		httpError(w, http.StatusTooManyRequests, "too many attempts, please wait")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	token := r.PathValue("token")
	u, purpose := s.livePasswordLink(token)
	if u == nil {
		passwordLinkInvalid(w)
		return
	}
	if msg := validatePassword(body.Password); msg != "" {
		httpError(w, 400, msg)
		return
	}
	s.loginSem <- struct{}{}
	hash := hashPassword(body.Password)
	<-s.loginSem
	// The DELETE is what claims the link: of two requests racing with the same
	// token, only one removes the row.
	res, err := s.db.Exec(`DELETE FROM password_links WHERE token_hash = ?`, tokenHash(token))
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		passwordLinkInvalid(w)
		return
	}
	if _, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, u.ID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.endPasswordKeys(u.ID)
	s.audit("human", u.ID, u.Name, "use_password_link_"+purpose, "", "", u.Name)
	writeJSON(w, map[string]string{"email": u.Email})
}
