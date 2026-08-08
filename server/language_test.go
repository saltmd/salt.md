package server

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The English-first rule, enforced on the Go side.
//
// The frontend has `npm run check`, which fails the build on a bare string or
// a drifted catalog. The server had nothing — and it showed: twelve finished
// German sentences were sitting in login redirects, plus two whole emails,
// none of which any frontend check could ever see. They were found by reading,
// which is exactly the "hunt through other people's commits" this project is
// meant to avoid.
//
// So the same bargain applies here. New German in a .go file fails the build.
//
// What counts as German is deliberately crude: an umlaut, or two or more words
// from a list that has no English homographs. Crude is fine — the goal is to
// catch a paragraph somebody wrote in German, not to identify a language.

var germanWords = regexp.MustCompile(`(?i)\b(und|nicht|wird|werden|sind|eine|einen|einem|einer|kann|muss|müssen|soll|sollen|beim|des|für|aus|nach|über|ohne|damit|weil|wenn|dann|noch|nur|schon|sich|hier|aber|oder|bitte|kein|keine|wurde|wurden|diese|dieser|dieses|jeder|jede|mehr|sehr|immer|wieder|zwischen|während|deshalb|sonst|bereits|jetzt|etwas|nichts|alles|ihre|seine|unser|gibt|geben|machen|macht|lassen|bleibt|steht|liegt|dabei|darauf|dafür|dadurch|daher|sowie|zum|zur|vom|beim)\b`) // i18n-ok: this list IS the detector

var umlauts = regexp.MustCompile(`[äöüßÄÖÜ]`) // i18n-ok: the letters are the subject

// germanStrong holds words that need no second witness: one of them in a line
// is German, full stop.
//
// The two-word rule above reads prose well and misses short remarks. A real
// one: "laufende Massenimporte (siehe …)" — i18n-ok: German example. It has no
// umlaut and one listed word, and it sat in server.go through three passes that
// all reported the file clean.
//
// Same trade as germanInString: no English homographs, because a check that
// cries wolf is a check somebody switches off.
var germanStrong = regexp.MustCompile(`(?i)\b(siehe|laufende|laufenden|einmalig|bringen|sichern|aktuelle|aktuellen|fassung|bedarf|alten|beim|damit|deshalb|jedoch|sondern|zuerst|bereits|gemeinsam|jeweils|nämlich|übrigens|zunächst|schliesslich|schließlich)\b`) // i18n-ok: this list IS the detector

// The marker is assembled from a constant instead of being written out in one
// piece. Spelled whole — marker, hyphen, "file", colon — it would match its OWN
// definition, and this file, the one that enforces the rule, would be the
// single file nobody checks. That is the shape a rule dies in.
//
// The same trap caught this very comment: an earlier draft spelled the token
// out to explain the problem, and re-created it. Do not name the token here.
const okMarker = "i18n-ok"

// exemptLine is the per-line escape hatch, same spelling as the frontend
// check. The reason is mandatory: a bare marker is how a rule quietly dies.
var exemptLine = regexp.MustCompile(okMarker + `:\s*\S`)

// exemptFile exempts a whole file, for the ones whose German IS the subject.
// Fixtures check that "Verträge" finds "Vertrag" — i18n-ok: that is the point.
// Translating them would delete the test.
var exemptFile = regexp.MustCompile(okMarker + `-file:\s*\S`)

// pendingTranslation lists the files whose comments have not been converted
// yet. It existed so the check could be switched on before the sweep was
// finished, and it was a debt list, not a config option.
//
// It is EMPTY, and that is the finished state: the sweep is done, every .go
// file in this tree reads as English. Putting a name back in here means taking
// on debt on purpose — and TestNoStalePendingTranslation will delete the entry
// again the moment the file is clean, so it cannot quietly become an allowlist.
var pendingTranslation = map[string]bool{}

func goSources(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// web/ has its own check; the rest is not ours.
			if name := info.Name(); name == "node_modules" || name == "dist" || name == ".git" || name == "web" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, filepath.ToSlash(strings.TrimPrefix(path, "../")))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(out)
	return out
}

// germanLines returns the 1-based line numbers that read as German.
func germanLines(t *testing.T, rel string) []int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	text := string(raw)
	if exemptFile.MatchString(text) {
		return nil
	}
	var hits []int
	for i, line := range strings.Split(text, "\n") {
		if exemptLine.MatchString(line) {
			continue
		}
		if umlauts.MatchString(line) || germanStrong.MatchString(line) ||
			len(germanWords.FindAllString(line, -1)) >= 2 {
			hits = append(hits, i+1)
		}
	}
	return hits
}

func TestSourceIsEnglish(t *testing.T) {
	for _, rel := range goSources(t) {
		hits := germanLines(t, rel)
		if len(hits) > 0 && !pendingTranslation[rel] {
			t.Errorf("%s: %d line(s) read as German, first at %s:%d\n"+
				"    Source text and comments are English (see CLAUDE.md).\n"+
				"    A line that is German on purpose says why: // i18n-ok: <reason>",
				rel, len(hits), rel, hits[0])
		}
	}
}

// TestNoStalePendingTranslation keeps the debt list honest. A file that has
// been converted has to leave the list in the same commit, or the list stops
// describing anything and starts hiding things.
func TestNoStalePendingTranslation(t *testing.T) {
	for rel := range pendingTranslation {
		if _, err := os.Stat(filepath.Join("..", rel)); err != nil {
			t.Errorf("%s is on pendingTranslation but does not exist", rel)
			continue
		}
		if len(germanLines(t, rel)) == 0 {
			t.Errorf("%s is already English — remove it from pendingTranslation", rel)
		}
	}
}

// The rule above catches PROSE. It cannot catch a SHORT string, and short is
// exactly the shape user-facing text has: "Nicht gefunden" is one German word
// and carries no umlaut, so it stays under the two-word threshold for ever.
//
// That is not theory. After the comment sweep read clean, seven German strings
// were still sitting in the source — a SECOND German 404 page served to
// anonymous visitors, the print bar of the HTML export, the admin test mail,
// and four mail errors. Every one of them was below the threshold.
//
// So string literals get their own, stricter rule: ONE unambiguous German word
// is enough. The list deliberately holds no English homographs — no "die",
// "war", "hat", "mit", "den", "in", "aus" — because a check that cries wolf is
// a check somebody switches off.
var germanInString = regexp.MustCompile(`(?i)\b(nicht|nichts|wird|werden|wurde|wurden|sind|eine|einen|einem|einer|kein|keine|muss|müssen|kann|darf|soll|sollen|bitte|danke|gefunden|verbunden|getrennt|gespeichert|gelöscht|geändert|angelegt|ungültig|fehlgeschlagen|erfolgreich|fehler|hinweis|achtung|passwort|benutzer|einstellungen|seiten|dateien|anmelden|abmelden|drucken|erforderlich|verfügbar|vorhanden|zugriff|berechtigung|speichern|abbrechen|weiter|zurück)\b`) // i18n-ok: this list IS the detector

// goString finds a Go string literal — interpreted or raw. Written in two
// pieces because the pattern itself contains a backtick.
var goString = regexp.MustCompile(`"(?:[^"\\\n]|\\.)*"` + "|`[^`]*`")

func TestUserFacingStringsAreEnglish(t *testing.T) {
	for _, rel := range goSources(t) {
		raw, err := os.ReadFile(filepath.Join("..", rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		text := string(raw)
		if exemptFile.MatchString(text) {
			continue
		}
		for i, line := range strings.Split(text, "\n") {
			if exemptLine.MatchString(line) || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, lit := range goString.FindAllString(line, -1) {
				if len([]rune(lit)) < 8 { // the two quotes plus six characters
					continue
				}
				if umlauts.MatchString(lit) || germanInString.MatchString(lit) {
					t.Errorf("%s:%d: string reads as German: %s\n"+
						"    Text a person reads is English at the source; the browser\n"+
						"    translates it from an error code (see serverErrors.ts).\n"+
						"    German on purpose says why: // %s: <reason>", rel, i+1, lit, okMarker)
					break
				}
			}
		}
	}
}
