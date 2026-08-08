package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Telling an admin that a newer release exists.
//
// This is the first thing a salt.md instance does on its own initiative that
// leaves the machine. Everything else outbound — webhooks, the calendar feed,
// the URL importer, the tunnel — happens because somebody configured it. So it
// is switchable, it is documented in wiki/automation.md beside the others, and
// it sends nothing: the request carries no instance id, no version, no counts,
// nothing but a GET for a public page anybody can open.
//
// It asks github.com, not api.github.com. The release page answers a plain 302
// whose Location carries the tag, and unlike the API it has no rate limit —
// 60 requests an hour per source IP would be shared by every instance behind
// one NAT, and the 403 that arrives when it runs out looks exactly like an
// answer.
const (
	updateRepo    = "saltmd/salt.md"
	updateLatest  = "https://github.com/" + updateRepo + "/releases/latest"
	updateEvery   = 24 * time.Hour
	updateTimeout = 10 * time.Second

	// app_settings keys. Values are TEXT, like every other setting.
	updateKeyChecked = "update_checked_at"
	updateKeyTag     = "update_latest_tag"
	updateKeyErr     = "update_last_error"
	updateKeySwitch  = "update_check"
)

// updateInfo is what an admin's browser gets. `Available` is the only field the
// banner needs; the rest is there so the answer can be read by a person with
// curl without a second call.
type updateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	URL       string `json:"url,omitempty"`
	CheckedAt string `json:"checkedAt,omitempty"`
	Enabled   bool   `json:"enabled"`
}

// updateCheckEnabled follows the house order: admin setting, then environment,
// then the default. On by default — an instance that silently misses security
// releases is the worse failure — and off in one click or one variable.
func (s *Server) updateCheckEnabled() bool {
	if v := s.setting(updateKeySwitch, ""); v != "" {
		return v != "off"
	}
	if v := Env("UPDATE_CHECK"); v != "" {
		return v != "0" && v != "off" && v != "false"
	}
	return true
}

// releaseVersion parses "v1.2.3" or "1.2.3" into comparable numbers. Anything
// else — "dev", a desktop tag, a date — returns false, which is what keeps this
// quiet on a build that was never released.
func releaseVersion(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	// A pre-release suffix is not something to compare, and not something to
	// offer: "1.2.3-rc1" is deliberately not the release anybody is waiting for.
	if strings.ContainsAny(s, "-+") {
		return out, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// isNewer is a plain semver comparison and nothing more. It exists because the
// same release is spelled "1.0.0" in a local build and "v1.0.0" in a released
// one, so comparing the strings would announce an update to itself.
func isNewer(latest, current string) bool {
	l, ok := releaseVersion(latest)
	if !ok {
		return false
	}
	c, ok := releaseVersion(current)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// latestReleaseTag reads the tag out of the redirect that
// github.com/<repo>/releases/latest answers with. The redirect is the answer,
// so it is deliberately not followed.
func (s *Server) latestReleaseTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, updateLatest, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{
		Timeout: updateTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", coded("update_no_redirect", "The release page did not answer with a redirect.")
	}
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return "", coded("update_no_tag", "The release redirect carried no tag.")
	}
	return loc[i+len("/tag/"):], nil
}

// checkForUpdate runs from the cleanup loop, so it is called every half hour
// and does nothing on almost all of those. A failure is stored rather than
// logged: on a machine with no internet this would otherwise print the same
// line forever, and an admin who wants to know can read it back from the
// endpoint.
func (s *Server) checkForUpdate() {
	if !s.updateCheckEnabled() {
		return
	}
	// A build nobody released has no newer version to be told about.
	if _, ok := releaseVersion(Version); !ok {
		return
	}
	if last := s.setting(updateKeyChecked, ""); last != "" {
		if t, err := time.Parse(time.RFC3339Nano, last); err == nil && time.Since(t) < updateEvery {
			return
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()

	tag, err := s.latestReleaseTag(ctx)
	s.setSetting(updateKeyChecked, now())
	if err != nil {
		s.setSetting(updateKeyErr, err.Error())
		return
	}
	s.setSetting(updateKeyErr, "")
	// A tag from another release line — the desktop app has its own — is not an
	// update to this server, and releaseVersion is what rejects it.
	if _, ok := releaseVersion(tag); ok {
		s.setSetting(updateKeyTag, tag)
	}
}

// handleUpdate answers the admin's browser. adminOnly already refuses everybody
// else, including API tokens, so the banner cannot leak to a member by mistake.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	tag := s.setting(updateKeyTag, "")
	info := updateInfo{
		Current:   Version,
		CheckedAt: s.setting(updateKeyChecked, ""),
		Enabled:   s.updateCheckEnabled(),
	}
	if info.Enabled && isNewer(tag, Version) {
		info.Available = true
		info.Latest = tag
		info.URL = "https://github.com/" + updateRepo + "/releases/tag/" + tag
	}
	writeJSON(w, info)
}
