package server

import (
	"net/http"
	"regexp"
	"strings"
)

// Locale preferences, per account (W112).
//
// Everything here already worked without a setting: the browser said which
// language it wanted, which regional format, which timezone, and Intl derived
// the clock and the first weekday from that. It is right almost always, and it
// stays the default.
//
// The setting exists for the "almost". Somebody reads German but wants ISO
// dates. Somebody works on a machine whose clock is on the wrong continent.
// Somebody is in Vienna on a laptop their employer set to en-US. None of those
// people could do anything about it, because there was no picker at all — the
// only way to change the language was to edit localStorage by hand.
//
// So every value gets two states: AUTOMATIC, which is what happens today, and
// MANUAL. Automatic is spelled as the empty string rather than a magic word,
// because "" is what a column has before anybody decides anything — the absence
// of a decision and the automatic mode are genuinely the same state, and giving
// them one representation means there is no third case to get wrong.
//
// They live on the ACCOUNT, not in localStorage. localStorage was the obvious
// place and is the wrong one: the phone and the laptop would disagree, which is
// the very complaint that prompted this. localStorage stays as a first-paint
// cache (see i18n.ts) so the first frame is not briefly in the wrong language,
// but the account is the truth.
type userPrefs struct {
	// Language to TRANSLATE into. "" follows navigator.languages.
	Language string `json:"language"`
	// Regional tag to FORMAT with — dates, numbers, sorting. Deliberately
	// separate from Language: Salt ships one catalog per language, but 'de-AT'
	// and 'de-DE' format differently, and somebody may want English text with
	// German dates. "" takes the browser's regional variant of Language.
	Region string `json:"region"`
	// IANA zone for moments ("Europe/Vienna"). "" is the system zone.
	// Calendar DAYS are never converted, whatever this says — see formatDay.
	TimeZone string `json:"timeZone"`
	// "" | "12" | "24". "" lets the region decide, which is what Intl does.
	Clock string `json:"clock"`
	// "" | "mon" | "sun" | "sat". "" lets the region decide.
	WeekStart string `json:"weekStart"`
}

var (
	// A language is a base tag: two or three letters. Anything the frontend
	// does not ship a catalog for falls back to English there, so an unknown
	// but well-formed tag is harmless.
	rxLanguage = regexp.MustCompile(`^[a-z]{2,3}$`)
	// A regional tag as BCP-47 writes them: 'de', 'de-AT', 'en-GB', 'sr-Latn-RS'.
	rxRegion = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)
	// A zone name by SHAPE only, not against a list.
	//
	// The binary carries no timezone database: CGO is off and `time/tzdata` is
	// not imported, so `time.LoadLocation` here would fail for every zone on a
	// machine without system tzdata and reject valid input. The browser is the
	// authority — it offers the list (Intl.supportedValuesOf) and it does the
	// formatting. This check only keeps junk and injection shapes out of the
	// column; format.ts falls back to automatic if a stored zone turns out to
	// be unknown, so a bad value degrades instead of breaking the page.
	rxTimeZone = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,2}$`)
)

// normalizePrefs cleans a submitted set and reports what was rejected.
//
// Rejection is silent-but-honest: an invalid value becomes automatic rather
// than an error, and the caller gets the cleaned set back so the interface
// shows what was actually stored. A settings dialog that refuses to save
// because one field is odd is worse than one that says "automatic" and means
// it.
func normalizePrefs(p userPrefs) userPrefs {
	out := userPrefs{
		Language:  strings.ToLower(strings.TrimSpace(p.Language)),
		Region:    strings.TrimSpace(p.Region),
		TimeZone:  strings.TrimSpace(p.TimeZone),
		Clock:     strings.TrimSpace(p.Clock),
		WeekStart: strings.ToLower(strings.TrimSpace(p.WeekStart)),
	}
	if !rxLanguage.MatchString(out.Language) {
		out.Language = ""
	}
	if len(out.Region) > 35 || !rxRegion.MatchString(out.Region) {
		out.Region = ""
	}
	if len(out.TimeZone) > 64 || !rxTimeZone.MatchString(out.TimeZone) {
		out.TimeZone = ""
	}
	if out.Clock != "12" && out.Clock != "24" {
		out.Clock = ""
	}
	switch out.WeekStart {
	case "mon", "sun", "sat":
	default:
		out.WeekStart = ""
	}
	return out
}

// loadPrefs reads an account's preferences. A missing row yields the zero
// value, which IS automatic — so a caller never has to check.
func (s *Server) loadPrefs(userID string) userPrefs {
	var p userPrefs
	s.db.QueryRow(`SELECT pref_language, pref_region, pref_timezone, pref_clock, pref_week_start
		FROM users WHERE id = ?`, userID).
		Scan(&p.Language, &p.Region, &p.TimeZone, &p.Clock, &p.WeekStart)
	return p
}

func (s *Server) savePrefs(userID string, p userPrefs) error {
	_, err := s.db.Exec(`UPDATE users SET pref_language = ?, pref_region = ?,
		pref_timezone = ?, pref_clock = ?, pref_week_start = ? WHERE id = ?`,
		p.Language, p.Region, p.TimeZone, p.Clock, p.WeekStart, userID)
	return err
}

// handlePutPrefs sets the caller's own preferences.
//
// Its own endpoint rather than another field on PATCH /api/users/{id}, for one
// reason: that route lets an ADMIN edit somebody else. Nobody should be able to
// set another person's clock format — it is not administration, it is reaching
// into how their screen looks. Here the account is the URL, so there is no
// "whose" to get wrong.
//
// The answer is the CLEANED set, not the submitted one. An unusable value comes
// back as automatic, and the dialog then shows what is actually stored instead
// of what was asked for.
func (s *Server) handlePutPrefs(w http.ResponseWriter, r *http.Request) {
	me := requestUser(r)
	var body userPrefs
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	clean := normalizePrefs(body)
	if err := s.savePrefs(me.ID, clean); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, clean)
}

// formatLocale is the tag dates and numbers are written with for one person —
// the same choice the app makes, so a printed document spells its date the way
// the screen does.
//
// Region first, then language, then nothing at all: an empty tag means the
// browser decides, which is exactly what "automatic" means everywhere else in
// these settings. Never a hard-coded default — that is how an instance ends up
// printing 2026-08-10 at a German desk.
func (s *Server) formatLocale(userID string) string {
	if userID == "" {
		return ""
	}
	p := s.loadPrefs(userID)
	if p.Region != "" {
		return p.Region
	}
	return p.Language
}
