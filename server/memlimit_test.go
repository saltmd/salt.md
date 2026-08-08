package server

import "testing"

// The sizing rule has to hold on machines this test never runs on, so it is
// checked as arithmetic rather than through the Linux-only detection.
//
// The case that matters most is the first one: a container that cannot read
// its own limit must behave like a SMALL machine, not an unlimited one. The
// outage came from a process that believed it had the host's memory.
func TestExtractLimitScalesWithMemory(t *testing.T) {
	const mb int64 = 1 << 20
	// Exact bytes, and asserted in bytes: rounding both sides to megabytes is
	// how "limit 5 MB, want 5 MB" turns into a failing test nobody can read.
	cases := []struct {
		name  string
		avail int64
		want  int64
	}{
		{"unknown machine falls back, does not assume plenty", 0, 10 * mb},
		{"tiny box gets the floor, not a useless 2 MB", 256 * mb, 5 * mb},
		{"512 MB sits just above the floor", 512 * mb, 512 * mb / 100},
		{"2 GB (the instance that fell over)", 2 << 30, (2 << 30) / 100},
		{"8 GB", 8 << 30, 50 * mb}, // 8 GB/100 = 82 MB, capped
		{"64 GB stays at the ceiling", 64 << 30, 50 * mb},
	}
	for _, c := range cases {
		if got := extractLimitFor(c.avail); got != c.want {
			t.Errorf("%s: %d bytes available → limit %d bytes, want %d",
				c.name, c.avail, got, c.want)
		}
	}
}

// The properties that must hold at every size, independent of the exact
// arithmetic: never below the floor, never above the ceiling, and monotonic —
// a bigger machine may never be allowed less than a smaller one.
func TestExtractLimitStaysWithinItsBounds(t *testing.T) {
	const mb int64 = 1 << 20
	var prev int64
	for _, avail := range []int64{1 * mb, 256 * mb, 512 * mb, 1 << 30, 2 << 30, 4 << 30, 8 << 30, 64 << 30} {
		got := extractLimitFor(avail)
		if got < 5*mb || got > 50*mb {
			t.Errorf("%d MB available → %d bytes, outside the 5–50 MB bounds", avail>>20, got)
		}
		if got < prev {
			t.Errorf("%d MB available → %d bytes, less than the smaller machine got (%d)", avail>>20, got, prev)
		}
		prev = got
	}
}

func TestExtractionSlotsScaleWithMemory(t *testing.T) {
	cases := []struct {
		avail int64
		want  int
	}{
		{0, 1}, // unknown: assume small
		{512 << 20, 1},
		{2 << 30, 1},
		{8 << 30, 2},
		{32 << 30, 3},
	}
	for _, c := range cases {
		if got := extractionSlotsFor(c.avail); got != c.want {
			t.Errorf("%d MB available → %d slots, want %d", c.avail>>20, got, c.want)
		}
	}
}

// The case a plain `docker run` produces: no --memory, a big host. Assuming
// the host's size there is how a small container talks itself into work that
// gets it killed — so an uncapped container counts as small, and the operator
// raises it deliberately.
func TestUncappedContainerAssumesSmall(t *testing.T) {
	if containerWithoutCap > 4<<30 {
		t.Errorf("the no-cap assumption is %d MB — that is not a careful default",
			containerWithoutCap>>20)
	}
	// It has to leave a usable instance behind, not switch indexing off.
	if extractLimitFor(containerWithoutCap) < 5<<20 {
		t.Errorf("at the no-cap assumption only %d bytes would be indexed",
			extractLimitFor(containerWithoutCap))
	}
	if extractionSlotsFor(containerWithoutCap) < 1 {
		t.Error("the no-cap assumption would leave zero extraction slots")
	}
}

// Whatever the machine, one extraction must never be allowed to build more
// text than we are willing to keep — otherwise the cap is decoration.
func TestLimitsStayCoherentAcrossMachineSizes(t *testing.T) {
	for _, avail := range []int64{0, 256 << 20, 2 << 30, 8 << 30, 128 << 30} {
		limit := extractLimitFor(avail)
		if maxIndexedTextBytes > limit {
			t.Errorf("%d MB available: indexed-text cap %d exceeds the file limit %d",
				avail>>20, maxIndexedTextBytes, limit)
		}
		if slots := extractionSlotsFor(avail); slots < 1 {
			t.Errorf("%d MB available: %d slots would deadlock every upload", avail>>20, slots)
		}
	}
}
