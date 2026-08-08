package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const sessionCookie = "salt_session"

// sessionCookieValue returns the session value from the cookie.
func sessionCookieValue(r *http.Request) (string, bool) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		return c.Value, true
	}
	return "", false
}

type user struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Avatar  string `json:"avatar"`
	IsAdmin bool   `json:"isAdmin"`
	// Disabled: deactivated — no sign-in, no session, but everything stays
	// attributable. The normal case when somebody leaves (see
	// lifecycle_account.go).
	Disabled bool `json:"disabled"`
	// OrgRole is the instance role: owner | admin | member. It comes from
	// org_members and is loaded alongside so the interface can show owner
	// actions without needing a second query for it.
	OrgRole string `json:"orgRole"`
	// TokenScope is set only when the request authenticated via an API token:
	// "write" (full) or "read" (read-only). Empty for cookie/session auth,
	// which is always full access. Not serialized.
	TokenScope string `json:"-"`
	// api | oauth — see the workspace-level rule in workspaces.go. Empty for a
	// browser session, which that rule never limits.
	TokenKind string `json:"-"`
	// TokenWorkspaces restricts a token to specific workspaces. nil means
	// unrestricted (all the user's workspaces); non-nil is the allow-list.
	// Cookie/session auth is always nil (unrestricted). Not serialized.
	TokenWorkspaces []string `json:"-"`
}

type ctxKey int

const userCtxKey ctxKey = 0

func requestUser(r *http.Request) *user {
	u, _ := r.Context().Value(userCtxKey).(*user)
	return u
}

// ---- password hashing (argon2id, PHC string format) ----

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
)

func hashPassword(password string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

// dummyHash is verified against for unknown emails so a login attempt costs
// the same argon2 work whether or not the account exists (no enumeration via
// timing). Computed once at startup.
var dummyHash = hashPassword("salt-dummy-password-placeholder")

func verifyPassword(password, phc string) bool {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// ---- sessions & API tokens ----

// Which KIND of credential a request arrived with. The workspace-level rule
// distinguishes them: a permanent API token and a signed-in grant are not the
// same promise, however similar their permissions look.
const (
	tokenKindAPI   = "api"
	tokenKindOAuth = "oauth"
)

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Server) createSession(userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	expires := time.Now().UTC().Add(time.Duration(s.sessionDays()) * 24 * time.Hour).Format(time.RFC3339Nano)
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash(token), userID, now(), expires)
	return token, err
}

func (s *Server) userByID(id string) *user {
	var u user
	var isAdmin, disabled int
	err := s.db.QueryRow(`SELECT u.id, u.email, u.name, u.color, u.avatar, u.is_admin, u.disabled, COALESCE(m.role, '')
		FROM users u LEFT JOIN org_members m ON m.user_id = u.id WHERE u.id = ?`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.Color, &u.Avatar, &isAdmin, &disabled, &u.OrgRole)
	if err != nil {
		return nil
	}
	u.IsAdmin = isAdmin != 0
	u.Disabled = disabled != 0
	if u.OrgRole == "" {
		// Account with no organisation row (legacy): the old column decides.
		u.OrgRole = roleMember
		if u.IsAdmin {
			u.OrgRole = roleAdmin
		}
	}
	return &u
}

// currentUser resolves the request's user from the session cookie or an
// API bearer token. Returns nil when unauthenticated.
func (s *Server) currentUser(r *http.Request) *user {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		tok := strings.TrimPrefix(auth, "Bearer ")
		// Guessing gets cut off before the lookup once an address has burned
		// through its budget of WRONG tokens. Only failures pay in, so an agent
		// hammering away with a good token is never touched by this.
		if s.tokenRate.exhausted(s.clientIP(r)) {
			return nil
		}
		// An OAuth access token first — it is the short-lived one and the one a
		// strict workspace will accept, so it must not be shadowed by a lookup
		// that happens to miss.
		if u := s.userForAccessToken(tok, s.clientIP(r)); u != nil {
			return u
		}
		var userID, id, scope, wsScope string
		err := s.db.QueryRow(`SELECT id, user_id, scope, workspace_scope FROM api_tokens WHERE token_hash = ?`, tokenHash(tok)).Scan(&id, &userID, &scope, &wsScope)
		if err == nil {
			// WHERE it was used from, not just when. A token that travels in a URL
			// (/mcp/{token}) cannot be kept secret — it sits in the connector's
			// config, in Cloudflare's logs and in whatever proxy is between. What
			// CAN be done is make a stranger using it obvious, and rotating cheap.
			s.db.Exec(`UPDATE api_tokens SET last_used_at = ?, last_used_ip = ? WHERE id = ?`,
				now(), s.clientIP(r), id)
			u := s.userByID(userID)
			if u != nil {
				if scope != "read" {
					scope = "write"
				}
				u.TokenScope = scope
				u.TokenKind = tokenKindAPI
				if strings.TrimSpace(wsScope) != "" {
					for _, w := range strings.Split(wsScope, ",") {
						if w = strings.TrimSpace(w); w != "" {
							u.TokenWorkspaces = append(u.TokenWorkspaces, w)
						}
					}
				}
			}
			return u
		}
		// A REJECTED token is throttled; a valid one is not. An agent makes
		// hundreds of calls a minute with a good token and must not be slowed,
		// while guessing has no reason to be free.
		s.tokenRate.allow(s.clientIP(r))
		s.logAuthFailure(r, "token")
		return nil
	}
	val, ok := sessionCookieValue(r)
	if !ok {
		return nil
	}
	var userID, expires string
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM sessions WHERE token_hash = ?`, tokenHash(val)).Scan(&userID, &expires)
	if err != nil {
		return nil
	}
	if exp, err := time.Parse(time.RFC3339Nano, expires); err != nil || time.Now().After(exp) {
		s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash(val))
		return nil
	}
	return s.userByID(userID)
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			httpError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// Deactivated means deactivated — even if a session slipped through when
		// the account was switched off, or a new one appeared via OAuth.
		if u.Disabled {
			httpErrorCode(w, http.StatusForbidden, "account_disabled", "This account has been deactivated.")
			return
		}
		// A read-only API token may not perform mutating HTTP methods. Cookie
		// sessions (TokenScope=="") are unaffected. This is the single REST
		// choke point that mirrors the per-tool MCP check.
		if u.TokenScope == "read" {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				httpError(w, http.StatusForbidden, "token is read-only")
				return
			}
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userCtxKey, u)))
	}
}

// adminOnly: instance administration. Additionally requires a browser sign-in
// — see sessionOnly. An admin token would otherwise be an admin pass for every
// agent it is handed to.
func (s *Server) adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return s.auth(s.sessionOnly(func(w http.ResponseWriter, r *http.Request) {
		if !requestUser(r).IsAdmin {
			httpError(w, http.StatusForbidden, "admin only")
			return
		}
		next(w, r)
	}))
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// isHTTPS reports whether the request reached us over TLS, directly or via a
// terminating proxy. Enables the cookie Secure flag without breaking plain
// HTTP LAN deployments.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil ||
		r.Header.Get("X-Forwarded-Proto") == "https" ||
		r.Header.Get("X-Forwarded-Ssl") == "on"
}

// ---- handlers ----

func (s *Server) userCount() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	n, err := s.userCount()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	u := s.currentUser(r)
	// Preferences travel here and NOT on the user object: that object also goes
	// out in member lists and over MCP, and somebody else's timezone is not the
	// workspace's business. The zero value is automatic, so an unauthenticated
	// answer needs no special case.
	var prefs userPrefs
	if u != nil {
		prefs = s.loadPrefs(u.ID)
	}
	writeJSON(w, map[string]any{
		"setupRequired":       n == 0,
		"authenticated":       u != nil,
		"user":                u,
		"prefs":               prefs,
		"version":             Version,
		"allowUserWorkspaces": s.loadSettings().AllowUserWorkspaces,
	})
}

var userColors = []string{
	"#2f7d4f", "#c4554d", "#3b6fb5", "#b58a3b", "#7d4fb0",
	"#3ba0a8", "#b5527e", "#6b8f3b", "#8a6650", "#5560c4",
}

// validUserColor accepts only the given palette or a plain #hex — no room for
// CSS functions (url(), expression(), …).
func validUserColor(c string) bool {
	for _, p := range userColors {
		if strings.EqualFold(c, p) {
			return true
		}
	}
	if len(c) != 4 && len(c) != 7 {
		return false
	}
	if c[0] != '#' {
		return false
	}
	for _, ch := range c[1:] {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return true
}

func (s *Server) nextColor() string {
	n, _ := s.userCount()
	return userColors[n%len(userColors)]
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.userCount()
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if n > 0 {
		httpError(w, 403, "setup already completed")
		return
	}
	var body struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if err := validateAccount(body.Name, body.Email, body.Password); err != "" {
		httpError(w, 400, err)
		return
	}
	// Serialize setup so two concurrent requests can't both mint an admin.
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if n, err := s.userCount(); err != nil || n > 0 {
		httpError(w, 403, "setup already completed")
		return
	}
	id := newID()
	_, err = s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?, 1, ?)`,
		id, strings.ToLower(strings.TrimSpace(body.Email)), strings.TrimSpace(body.Name), s.nextColor(), hashPassword(body.Password), now())
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// The first admin gets a workspace and is its admin member. Reuse an
	// existing (upgrade-migrated) workspace if one is already present.
	var wsID string
	if s.db.QueryRow(`SELECT id FROM workspaces ORDER BY created_at LIMIT 1`).Scan(&wsID) != nil || wsID == "" {
		wsID = newID()
		// auto_join: the space the instance shares. The entry point behaves as it
		// always did — except it is now a visible decision the owner can switch
		// off again from the workspace menu.
		s.db.Exec(`INSERT INTO workspaces (id, name, created_at, owner_id, auto_join) VALUES (?, 'Workspace', ?, ?, 1)`, wsID, now(), id)
	} else {
		s.db.Exec(`UPDATE workspaces SET owner_id = ? WHERE id = ? AND owner_id = ''`, id, wsID)
	}
	s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin') ON CONFLICT DO NOTHING`, wsID, id)
	// The organisation is the level above workspaces; whoever sets the instance
	// up is its owner. The same flow carries a hosted signup later — with one
	// organisation per customer instead of exactly one.
	orgID := s.defaultOrg()
	if orgID == "" {
		orgID = newID()
		s.db.Exec(`INSERT INTO organizations (id, name, created_at) VALUES (?, ?, ?)`, orgID, "salt.md", now())
	}
	s.db.Exec(`INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`, orgID, id, roleOwner)
	// Claim any orphaned pages (e.g. the seeded welcome page created before a
	// workspace existed) into this workspace, owned by the first admin.
	s.db.Exec(`UPDATE pages SET workspace_id = ?, owner_id = COALESCE(NULLIF(owner_id,''), ?) WHERE workspace_id = ''`, wsID, id)
	token, err := s.createSession(id)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	setSessionCookie(w, r, token, s.sessionDays()*24*3600)
	writeJSON(w, s.userByID(id))
}

func validateAccount(name, email, password string) string {
	if strings.TrimSpace(name) == "" {
		return "name is required"
	}
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return "a valid email is required"
	}
	if len(password) < 8 {
		return "password must be at least 8 characters"
	}
	return ""
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Code     string `json:"code"` // TOTP, when 2FA is enabled
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	// Brute-force throttle per client IP (token bucket): stops password guessing
	// / spraying before it reaches the (expensive) argon2 verification.
	if !s.loginRate.allow(s.clientIP(r)) {
		httpError(w, http.StatusTooManyRequests, "too many login attempts, please wait")
		return
	}
	// Bound concurrent password verifications: argon2 is intentionally
	// memory-heavy (64 MiB each), so unbounded parallel logins could exhaust
	// RAM. Acquire the slot BEFORE hashing.
	s.loginSem <- struct{}{}
	defer func() { <-s.loginSem }()

	var id, hash, totpSecret string
	var totpEnabled, disabled int
	err := s.db.QueryRow(`SELECT id, password_hash, totp_secret, totp_enabled, disabled FROM users WHERE email = ?`,
		strings.ToLower(strings.TrimSpace(body.Email))).Scan(&id, &hash, &totpSecret, &totpEnabled, &disabled)
	if err != nil {
		hash = dummyHash // verify anyway so timing doesn't reveal account existence
	}
	if !verifyPassword(body.Password, hash) || err != nil {
		s.logAuthFailure(r, "password")
		httpErrorCode(w, http.StatusUnauthorized, "bad_credentials", "Wrong email or wrong password.")
		return
	}
	// Deactivated account: refuse only AFTER the password check, or the response
	// would give away that this address exists at all.
	if disabled != 0 {
		httpErrorCode(w, http.StatusForbidden, "account_disabled", "This account has been deactivated — talk to an admin.")
		return
	}
	// Second factor: password was correct, now require a valid TOTP code. The
	// distinct 401 body lets the client show a code field without re-asking for
	// the password.
	if totpEnabled != 0 {
		if body.Code == "" {
			httpErrorCode(w, http.StatusUnauthorized, "2fa_required", "Please enter the 6-digit code from your authenticator app.")
			return
		}
		if !verifyTOTP(totpSecret, body.Code) {
			httpErrorCode(w, http.StatusUnauthorized, "2fa_invalid", "Wrong code — try again.")
			return
		}
	}
	token, err := s.createSession(id)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	setSessionCookie(w, r, token, s.sessionDays()*24*3600)
	writeJSON(w, s.userByID(id))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if val, ok := sessionCookieValue(r); ok {
		s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash(val))
	}
	setSessionCookie(w, r, "", -1)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT u.id, u.email, u.name, u.color, u.avatar, u.is_admin, u.disabled, COALESCE(m.role, '')
		FROM users u LEFT JOIN org_members m ON m.user_id = u.id ORDER BY u.created_at`)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	users := []user{}
	for rows.Next() {
		var u user
		var isAdmin, disabled int
		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Color, &u.Avatar, &isAdmin, &disabled, &u.OrgRole); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		u.IsAdmin = isAdmin != 0
		u.Disabled = disabled != 0
		if u.OrgRole == "" {
			u.OrgRole = roleMember
			if u.IsAdmin {
				u.OrgRole = roleAdmin
			}
		}
		users = append(users, u)
	}
	writeJSON(w, users)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string `json:"name"`
		Email      string `json:"email"`
		Password   string `json:"password"`
		IsAdmin    bool   `json:"isAdmin"`
		Workspaces []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"workspaces"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if msg := validateAccount(body.Name, body.Email, body.Password); msg != "" {
		httpError(w, 400, msg)
		return
	}
	id := newID()
	admin := 0
	if body.IsAdmin {
		admin = 1
	}
	_, err := s.db.Exec(`INSERT INTO users (id, email, name, color, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, strings.ToLower(strings.TrimSpace(body.Email)), strings.TrimSpace(body.Name), s.nextColor(), hashPassword(body.Password), admin, now())
	if err != nil {
		httpError(w, 400, "email already in use")
		return
	}
	s.addOrgMember(id, body.IsAdmin)
	me := requestUser(r)
	if len(body.Workspaces) > 0 {
		// Explicit assignment: these workspaces only, with the chosen role.
		//
		// And only ones the creator is actually allowed to grant. Without this
		// check the entire separation of rights would be pointless: an admin
		// creates an account with a password of their choosing, puts it into
		// somebody else's workspace as a workspace admin, and signs in as it.
		for _, ws := range body.Workspaces {
			if ws.Role == "none" || ws.ID == "" {
				continue
			}
			if !s.isOwner(me.ID) && !s.isWorkspaceAdmin(me.ID, ws.ID) {
				continue
			}
			// The user was just created; an unknown workspace id falls out at the
			// foreign key, harmlessly.
			s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
				ws.ID, id, normalizeRole(ws.Role))
		}
	}
	// Their own space plus the workspaces open to everyone — regardless of what
	// was selected above.
	//
	// An account with NO selection used to inherit every workspace of the admin
	// who created it. Since an admin is usually a member everywhere, every
	// newcomer saw everything at once — which is the observation this whole wave
	// started from.
	s.onboardUser(id, body.Name)
	writeJSON(w, s.userByID(id))
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	me := requestUser(r)
	if me.ID != id && !me.IsAdmin {
		httpError(w, 403, "forbidden")
		return
	}
	var body struct {
		Name            *string `json:"name"`
		Email           *string `json:"email"`
		Color           *string `json:"color"`
		Avatar          *string `json:"avatar"`
		Password        *string `json:"password"`
		CurrentPassword *string `json:"currentPassword"`
		IsAdmin         *bool   `json:"isAdmin"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	// ---- CHECK EVERYTHING FIRST, then apply everything. Each field used to be
	// committed on its own; if a later one failed its check, the row was left
	// half changed (isAdmin already set, say, while the call returned 409).
	changingSensitive := body.Password != nil || body.Email != nil
	if me.ID == id && changingSensitive {
		var hash string
		s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
		if body.CurrentPassword == nil || !verifyPassword(*body.CurrentPassword, hash) {
			httpError(w, 403, "current password is incorrect")
			return
		}
	}
	// Setting SOMEBODY ELSE'S password means being able to sign in as that
	// person and read everything they see. That is no longer user management but
	// data access — and so stays with the owner, who has the file anyway. The
	// same holds for somebody else's email: it decides their future SSO
	// identity.
	if me.ID != id && changingSensitive && !s.isOwner(me.ID) {
		httpErrorCode(w, 403, "owner_only_credentials", "Only the owner can change another account's password or email. As an admin you can send an invitation.")
		return
	}
	if body.IsAdmin != nil && me.IsAdmin && !*body.IsAdmin {
		// Taking your OWN admin rights away locks you out of the administration
		// dialog you have open (every further action falls to 403) — and you
		// cannot promote yourself back. So: forbidden.
		if me.ID == id {
			httpError(w, 400, "you cannot remove your own admin rights — ask another admin")
			return
		}
		var others int
		s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND id != ?`, id).Scan(&others)
		if others == 0 {
			httpError(w, 400, "cannot remove the last admin")
			return
		}
		// Taking is_admin from the owner would leave them half locked out: they
		// would keep the owner role, but every adminOnly route would be shut.
		if s.isOwner(id) {
			httpErrorCode(w, 400, "owner_rights_locked", "The owner's rights cannot be revoked — hand the owner role on first.")
			return
		}
	}
	var newEmail string
	if body.Email != nil {
		newEmail = strings.ToLower(strings.TrimSpace(*body.Email))
		if !strings.Contains(newEmail, "@") || len(newEmail) < 5 || strings.ContainsAny(newEmail, " \t") {
			httpError(w, 400, "that does not look like an email address")
			return
		}
		var clash int
		s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = ? AND id != ?`, newEmail, id).Scan(&clash)
		if clash > 0 {
			httpError(w, 409, "another account already uses this email")
			return
		}
	}
	var newAvatar string
	if body.Avatar != nil {
		newAvatar = strings.TrimSpace(*body.Avatar)
		if newAvatar != "" && (!strings.HasPrefix(newAvatar, "/files/") || strings.ContainsAny(newAvatar, "()'\"")) {
			httpError(w, 400, "avatar must be an uploaded file")
			return
		}
	}
	if body.Color != nil && !validUserColor(*body.Color) {
		httpError(w, 400, "invalid color")
		return
	}
	if body.Password != nil && len(*body.Password) < 8 {
		httpError(w, 400, "password must be at least 8 characters")
		return
	}

	// ---- Everything is checked from here; now apply it.
	if body.IsAdmin != nil && me.IsAdmin {
		v := 0
		role := roleMember
		if *body.IsAdmin {
			v = 1
			role = roleAdmin
		}
		s.db.Exec(`UPDATE users SET is_admin = ? WHERE id = ?`, v, id)
		// Carry the instance role along, or the two drift apart: the fallback in
		// orgRole only fires on a MISSING row, not on a stale one. An owner is
		// never touched here — the check above prevents it.
		if org := s.defaultOrg(); org != "" {
			s.db.Exec(`UPDATE org_members SET role = ? WHERE org_id = ? AND user_id = ? AND role != ?`, role, org, id, roleOwner)
		}
	}
	if body.Email != nil {
		s.db.Exec(`UPDATE users SET email = ?, email_verified = 0 WHERE id = ?`, newEmail, id)
	}
	if body.Avatar != nil {
		s.db.Exec(`UPDATE users SET avatar = ? WHERE id = ?`, newAvatar, id)
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		s.db.Exec(`UPDATE users SET name = ? WHERE id = ?`, strings.TrimSpace(*body.Name), id)
	}
	if body.Color != nil {
		s.db.Exec(`UPDATE users SET color = ? WHERE id = ?`, *body.Color, id)
	}
	if body.Password != nil {
		s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hashPassword(*body.Password), id)
		// Changing the password invalidates all of the account's sessions and tokens.
		s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, id)
		s.db.Exec(`DELETE FROM api_tokens WHERE user_id = ?`, id)
		if me.ID == id {
			if token, err := s.createSession(id); err == nil {
				setSessionCookie(w, r, token, s.sessionDays()*24*3600)
			}
		}
	}
	writeJSON(w, s.userByID(id))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == requestUser(r).ID {
		httpError(w, 400, "you cannot delete yourself")
		return
	}
	var admins int
	s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND id != ?`, id).Scan(&admins)
	if admins == 0 {
		httpError(w, 400, "cannot delete the last admin")
		return
	}
	// Do not delete the owner. Their org_members row would go with them by
	// CASCADE, and the migration only grants the role to accounts WITHOUT a row
	// — the instance would be left permanently ownerless: no emergency access,
	// no password reset, no instance backup. Repairable only by hand in the
	// database. Hand it over first, then delete.
	if s.isOwner(id) {
		httpErrorCode(w, 400, "owner_cannot_be_deleted", "The owner cannot be deleted — hand the owner role to another account first.")
		return
	}
	// Order: first RECORD what hangs off the account (after this the memberships
	// are gone by CASCADE and nobody would know any more), then delete the
	// account, then carry the plan out. The other way round — destroy first,
	// then delete — a failed DELETE ended in the worst possible state: content
	// gone beyond recovery, account still able to sign in.
	me := requestUser(r)
	plan := s.deletionImpactOf(id)
	if plan.Err != nil {
		// An empty plan would look like "nothing hangs off this" and leave every
		// one of the account's workspaces orphaned. Better not to delete at all.
		httpErrorCode(w, 500, "impact_unavailable", "The consequences of this deletion could not be determined — please try again.")
		return
	}
	target := s.userByID(id)
	if _, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// The calendar subscription link lives as an app_settings row and hangs off
	// no foreign key — it would otherwise outlive the account.
	s.db.Exec(`DELETE FROM app_settings WHERE key = ?`, "ics_token_"+id)
	s.applyDeletion(plan, id, me.ID, me.Name)
	if target != nil {
		s.audit("human", me.ID, me.Name, "delete_user", "", "", target.Name)
	}
	// Handed-over and deleted workspaces change the sidebar of every open
	// session — without this signal the owner would only see the new workspace
	// after a reload, and deleted pages stayed visibly clickable.
	s.pagesChanged()
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- API tokens ----

type apiToken struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Scope      string   `json:"scope"`
	Workspaces []string `json:"workspaces"` // empty = all the user's workspaces
	CreatedAt  string   `json:"createdAt"`
	LastUsedAt *string  `json:"lastUsedAt"`
	// Where from. The point of recording it is that somebody can SEE it — a
	// token that rides in a URL is defended by noticing, not by secrecy.
	LastUsedIP string `json:"lastUsedIp"`
}

func (s *Server) handleListTokens(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, name, scope, workspace_scope, created_at, last_used_at, last_used_ip FROM api_tokens WHERE user_id = ? ORDER BY created_at`, requestUser(r).ID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	tokens := []apiToken{}
	for rows.Next() {
		var t apiToken
		var wsScope string
		if err := rows.Scan(&t.ID, &t.Name, &t.Scope, &wsScope, &t.CreatedAt, &t.LastUsedAt, &t.LastUsedIP); err != nil {
			httpError(w, 500, err.Error())
			return
		}
		t.Workspaces = []string{}
		for _, wid := range strings.Split(wsScope, ",") {
			if wid = strings.TrimSpace(wid); wid != "" {
				t.Workspaces = append(t.Workspaces, wid)
			}
		}
		tokens = append(tokens, t)
	}
	writeJSON(w, tokens)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string   `json:"name"`
		Scope      string   `json:"scope"`
		Workspaces []string `json:"workspaces"` // empty = all the user's workspaces
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		body.Name = "API token"
	}
	scope := "write"
	if body.Scope == "read" {
		scope = "read"
	}
	// Workspace scope: keep only ids the caller is actually a member of, so a
	// token can never be minted for a workspace the user cannot reach. Empty →
	// unrestricted (all the user's current + future workspaces).
	userID := requestUser(r).ID
	member := map[string]bool{}
	for _, wid := range s.visibleWorkspaces(userID) {
		member[wid] = true
	}
	seen := map[string]bool{}
	var scoped []string
	for _, wid := range body.Workspaces {
		wid = strings.TrimSpace(wid)
		if wid != "" && member[wid] && !seen[wid] {
			seen[wid] = true
			scoped = append(scoped, wid)
		}
	}
	// FAIL CLOSED: the caller asked to restrict to specific workspaces but none
	// of them survived the membership filter — do NOT store an empty scope, which
	// reads back as "unrestricted" (all workspaces). Reject instead, so a
	// deliberately-narrowed token can never silently become maximally privileged.
	if len(body.Workspaces) > 0 && len(scoped) == 0 {
		httpError(w, 400, "none of the selected workspaces are available to you")
		return
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	token := "salt_" + hex.EncodeToString(b)
	id := newID()
	_, err := s.db.Exec(`INSERT INTO api_tokens (id, user_id, name, token_hash, scope, workspace_scope, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, strings.TrimSpace(body.Name), tokenHash(token), scope, strings.Join(scoped, ","), now())
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	// The clear-text token is returned exactly once.
	writeJSON(w, map[string]any{"id": id, "token": token, "scope": scope, "workspaces": scoped})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.Exec(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, r.PathValue("id"), requestUser(r).ID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpError(w, 404, "token not found")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// deleteExpiredSessions is a small housekeeping pass run at startup.
func (s *Server) deleteExpiredSessions() {
	s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now())
}
