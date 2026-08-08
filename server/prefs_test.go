package server

import "testing"

// normalizePrefs is the only thing standing between a submitted JSON body and a
// column that the frontend later hands to Intl. Its job is not to be clever —
// it is to make sure that whatever comes out is either something usable or the
// empty string, and never a third thing.
func TestNormalizePrefs(t *testing.T) {
	cases := []struct {
		name string
		in   userPrefs
		want userPrefs
	}{
		{
			name: "the empty set is automatic and stays that way",
			in:   userPrefs{},
			want: userPrefs{},
		},
		{
			name: "a full manual set survives intact",
			in:   userPrefs{Language: "de", Region: "de-AT", TimeZone: "Europe/Vienna", Clock: "24", WeekStart: "mon"},
			want: userPrefs{Language: "de", Region: "de-AT", TimeZone: "Europe/Vienna", Clock: "24", WeekStart: "mon"},
		},
		{
			name: "surrounding space and case are the caller's slip, not a rejection",
			in:   userPrefs{Language: " DE ", Region: " de-AT ", TimeZone: " Europe/Vienna ", Clock: " 24 ", WeekStart: " MON "},
			want: userPrefs{Language: "de", Region: "de-AT", TimeZone: "Europe/Vienna", Clock: "24", WeekStart: "mon"},
		},
		{
			// Region keeps its case: BCP-47 writes the region subtag upper and
			// the script subtag capitalised, and Intl is fussier than the spec.
			name: "a three-part regional tag keeps its shape",
			in:   userPrefs{Region: "sr-Latn-RS"},
			want: userPrefs{Region: "sr-Latn-RS"},
		},
		{
			name: "a language we ship no catalog for is well formed and allowed",
			in:   userPrefs{Language: "fr"},
			want: userPrefs{Language: "fr"},
		},
		{
			name: "an unusable value becomes automatic rather than an error",
			in:   userPrefs{Language: "deutsch", Region: "!!", TimeZone: "nope nope", Clock: "13", WeekStart: "tue"},
			want: userPrefs{},
		},
		{
			// The shape rule exists to keep this out of the column. It cannot
			// be reached through the API anyway, but the column is also read
			// back into an Intl option, and that is worth one regexp.
			name: "an injection-shaped zone is refused",
			in:   userPrefs{TimeZone: "Europe/Vienna'); DROP TABLE users;--"},
			want: userPrefs{},
		},
		{
			name: "an over-long zone is refused even though its shape is fine",
			in:   userPrefs{TimeZone: "Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/Bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			want: userPrefs{},
		},
		{
			// Three levels happen: "America/Argentina/Salta".
			name: "a three-level zone is accepted",
			in:   userPrefs{TimeZone: "America/Argentina/Salta"},
			want: userPrefs{TimeZone: "America/Argentina/Salta"},
		},
		{
			name: "UTC and offset-style zones are accepted",
			in:   userPrefs{TimeZone: "UTC"},
			want: userPrefs{TimeZone: "UTC"},
		},
		{
			// One bad field must not take the good ones with it. A dialog that
			// silently discards four correct settings because the fifth was odd
			// is the worst of both worlds.
			name: "a bad field does not drag the others down",
			in:   userPrefs{Language: "de", Region: "de-AT", TimeZone: "not a zone!", Clock: "24", WeekStart: "mon"},
			want: userPrefs{Language: "de", Region: "de-AT", TimeZone: "", Clock: "24", WeekStart: "mon"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := normalizePrefs(c.in)
			if got != c.want {
				t.Errorf("normalizePrefs(%+v)\n got  %+v\n want %+v", c.in, got, c.want)
			}
		})
	}
}

// Cleaning has to be a fixed point: feeding a cleaned set back in may not
// change it again. Without that, saving twice could quietly produce a different
// result from saving once.
func TestNormalizePrefsIsStable(t *testing.T) {
	for _, in := range []userPrefs{
		{},
		{Language: "de", Region: "de-AT", TimeZone: "Europe/Vienna", Clock: "24", WeekStart: "mon"},
		{Language: "DEUTSCH", Region: "  en-GB", TimeZone: "bad zone", Clock: "12", WeekStart: "sat"},
	} {
		once := normalizePrefs(in)
		twice := normalizePrefs(once)
		if once != twice {
			t.Errorf("not stable for %+v:\n once  %+v\n twice %+v", in, once, twice)
		}
	}
}
