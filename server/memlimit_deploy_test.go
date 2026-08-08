package server

import (
	"fmt"
	"testing"
)

// The shapes salt.md actually gets deployed into, walked through end to end:
// what each source says, what comes out, and what that means for indexing.
//
// The owner's instance is the awkward one — Docker inside an LXC inside
// Proxmox. There the inner container sees neither the LXC's cap (its own
// cgroup says "unlimited") nor a truthful /proc/meminfo (that reports the
// Proxmox host's 64 GB). No amount of reading files fixes that; only the
// operator can say what the share is. This test exists so the consequence of
// each choice is written down rather than discovered on a live box.
func TestRealDeploymentShapes(t *testing.T) {
	const mb int64 = 1 << 20
	const gb int64 = 1 << 30

	cases := []struct {
		shape     string
		declared  int64 // SALT_MEMORY_MB
		cgroupCap int64 // enforced container limit
		host      int64 // what /proc/meminfo reports
		container bool
		want      int64
	}{
		{
			shape: "bare metal / systemd — /proc/meminfo is the truth",
			host:  8 * gb, want: 8 * gb,
		},
		{
			shape:     "plain `docker run`, no --memory, big host",
			host:      64 * gb,
			container: true,
			want:      containerWithoutCap, // NOT 64 GB — the host is not a promise
		},
		{
			shape:     "docker run --memory=14g",
			cgroupCap: 14 * gb,
			host:      64 * gb,
			container: true,
			want:      14 * gb,
		},
		{
			shape:     "the owner's box: Docker in a 16 GB LXC on a 64 GB Proxmox host, no --memory",
			host:      63413 * mb, // the Proxmox host's figure, not the LXC's
			container: true,
			want:      containerWithoutCap,
		},
		{
			shape:     "same box, SALT_MEMORY_MB=14000",
			declared:  14000 * mb,
			host:      63413 * mb,
			container: true,
			want:      14000 * mb,
		},
		{
			shape:     "small host, uncapped container — the host figure is still the ceiling",
			host:      1 * gb,
			container: true,
			want:      1 * gb, // must not round UP to the 2 GB assumption
		},
		// A fresh install on a VPS, the most common shape after the owner's.
		// Bare metal or KVM: nothing lies, /proc/meminfo is the machine.
		{shape: "VPS 2 GB, systemd install", host: 2 * gb, want: 2 * gb},
		{shape: "VPS 8 GB, systemd install", host: 8 * gb, want: 8 * gb},
		{shape: "VPS 16 GB, systemd install", host: 16 * gb, want: 16 * gb},
		// LXC without Docker (the test box): lxcfs masks /proc/meminfo to the
		// container's own size and /proc/1/cgroup looks like bare metal, so
		// the reading is correct and the assumption never engages. Measured on
		// A 4 GB LXC reports 4096 MB.
		{shape: "LXC 4 GB, systemd install (measured)", host: 4 * gb, want: 4 * gb},
		// Docker on a dedicated VPS, no --memory. The container has the
		// machine to itself, but cannot prove that — so it gets the careful
		// assumption and the startup line telling the operator how to raise
		// it. This is the case where the caution costs something real.
		{shape: "VPS 8 GB, plain docker run (caution costs here)",
			host: 8 * gb, container: true, want: containerWithoutCap},
		{shape: "VPS 8 GB, docker run --memory=7g",
			cgroupCap: 7 * gb, host: 8 * gb, container: true, want: 7 * gb},
	}

	for _, c := range cases {
		got := resolveMemory(c.declared, c.cgroupCap, c.host, c.container)
		if got != c.want {
			t.Errorf("%s:\n  got %d MB, want %d MB", c.shape, got>>20, c.want>>20)
			continue
		}
		t.Logf("%-72s → %5d MB · PDF bis %2d MB · %d Extraktion(en)",
			c.shape, got>>20, extractLimitFor(got)>>20, extractionSlotsFor(got))
	}
}

// Whatever a machine reports, the answer must stay inside the bounds the rest
// of the code assumes — no source may push it past them.
func TestResolveNeverEscapesItsBounds(t *testing.T) {
	const gb int64 = 1 << 30
	for _, host := range []int64{0, 1 * gb, 64 * gb, 1 << 50} {
		for _, cap := range []int64{0, 512 << 20, 14 * gb} {
			for _, container := range []bool{true, false} {
				got := resolveMemory(0, cap, host, container)
				limit := extractLimitFor(got)
				if limit < 5<<20 || limit > 50<<20 {
					t.Errorf("host=%d cap=%d container=%v → %s outside bounds",
						host, cap, container, fmt.Sprint(limit))
				}
			}
		}
	}
}
