package server

import (
	"log"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
)

// How much memory this process may actually use, and what that means for the
// expensive work it is allowed to take on.
//
// The trap this exists for: inside a container, /proc/meminfo reports the
// HOST's memory. A container capped at 512 MB on a 64 GB machine believes it
// has 64 GB, sizes its work accordingly, and gets killed by the first large
// document. The cgroup files below are the only place the real cap is written.

// containerWithoutCap is what a plain `docker run ghcr.io/saltmd/salt.md`
// produces: a container with no --memory, on a host with plenty. There the
// host's figure is NOT a promise — the operator may hand this container 512 MB
// tomorrow, and nothing in the container would notice. Assuming the host's
// size is how a 512 MB container on a 64 GB machine talks itself into work
// that gets it killed.
//
// So an uncapped container is treated as a small one. Being wrong in this
// direction costs a bit of search coverage on a big machine; being wrong the
// other way costs the server. Operators who know better say so, either with
// --memory (which is worth setting anyway) or with SALT_MEMORY_MB.
const containerWithoutCap = 2 << 30 // 2 GiB

// inContainer reports whether we are inside a container runtime. /.dockerenv
// covers Docker and Podman; the cgroup path catches the rest.
func inContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	b, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "kubepods") ||
		strings.Contains(s, "containerd") || strings.Contains(s, "lxc")
}

// availableMemory returns the memory ceiling in bytes, or 0 when it cannot be
// determined. In order: what the operator declared, what the container runtime
// enforces, and only then what the machine reports.
func availableMemory() int64 {
	// The operator's word beats every guess — and it is the only answer for
	// nested setups (LXC → Docker), where the inner container can see neither
	// the outer cap nor a truthful /proc/meminfo.
	if v := Env("MEMORY_MB"); v != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && n > 0 {
			return n << 20
		}
		log.Printf("memory: SALT_MEMORY_MB=%q is not a positive number of megabytes — ignoring it", v)
	}
	for _, p := range []string{
		"/sys/fs/cgroup/memory.max",                   // cgroup v2
		"/sys/fs/cgroup/memory/memory.limit_in_bytes", // cgroup v1
	} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(b))
		// v2 writes "max" when unlimited; v1 writes a number so large it means
		// the same thing. Both mean "this container has no cap".
		if s == "max" {
			break
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || n <= 0 || n > 1<<50 {
			break
		}
		return n
	}
	return resolveMemory(0, 0, hostMemory(), inContainer())
}

// resolveMemory is the decision on its own, so it can be checked against real
// deployment shapes without needing that deployment. All four inputs in bytes
// except the flag; 0 means "this source had nothing to say".
//
//	declared  — SALT_MEMORY_MB, the operator's word
//	cgroupCap — an enforced container limit (0 when unlimited or unreadable)
//	host      — what /proc/meminfo reports, which inside a container may be
//	            the machine rather than our share of it
func resolveMemory(declared, cgroupCap, host int64, container bool) int64 {
	if declared > 0 {
		return declared
	}
	if cgroupCap > 0 {
		return cgroupCap
	}
	if container {
		// No cap and we are in a container: the host figure describes the
		// machine, not our share. Take the smaller of assumption and host —
		// on a genuinely small host the host figure is still the real ceiling.
		if host > 0 && host < containerWithoutCap {
			return host
		}
		return containerWithoutCap
	}
	return host
}

// hostMemory reads the machine's total memory from /proc/meminfo. Linux only,
// which is where salt.md is deployed; elsewhere (a developer's Mac) it returns
// 0 and every caller falls back to its conservative default. Guessing a figure
// would be worse than admitting we do not know one.
func hostMemory() int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		f := strings.Fields(line) // "MemTotal:", "16777216", "kB"
		if len(f) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb << 10
	}
	return 0
}

// applyMemoryLimit tells the garbage collector where the ceiling is instead of
// letting it guess. Without this the collector sizes itself against the host's
// memory and lets the heap grow past a container cap — it collects too late,
// and the kernel gets there first. 80% leaves room for everything that is not
// Go heap (stacks, the SQLite library, file buffers).
func applyMemoryLimit() {
	avail := availableMemory()
	if avail <= 0 {
		return
	}
	debug.SetMemoryLimit(avail / 100 * 80)
	log.Printf("memory: %d MB available, soft limit %d MB, PDF indexing up to %d MB, %d extraction(s) at a time",
		avail>>20, (avail/100*80)>>20, pdfExtractLimit()>>20, extractionSlots())
	// Say why, and how to change it. Without this line an operator on a large
	// machine sees a small figure, has no way to tell an assumption from a
	// reading, and cannot know that one flag fixes it.
	if Env("MEMORY_MB") == "" && inContainer() && avail == containerWithoutCap {
		log.Printf("memory: no container limit is set, so this assumes a small instance. " +
			"Run with --memory=<size> (recommended) or set SALT_MEMORY_MB=<megabytes> " +
			"to use more — it only affects how much gets indexed, never whether an upload succeeds.")
	}
}

// pdfExtractLimit is the largest PDF whose text we are willing to build in
// memory. A parser allocates a multiple of the file's size for its object
// tree, and the server has to keep answering everything else meanwhile — hence
// a hundredth of what is available, not a half.
//
// This scales automatically because getting it wrong is only ever a graceful
// degradation: the file is still stored, still listed, still downloadable, and
// only its text stays out of the search index. Never a failed upload, so
// nobody is left wondering why the same file behaved differently on two
// machines. The upload limit itself deliberately does NOT scale — see
// maxUploadBytes.
func pdfExtractLimit() int64 { return extractLimitFor(availableMemory()) }

// extractLimitFor is the arithmetic on its own, so the sizing can be tested
// without a container: the detection reads files that exist only on Linux, but
// the rule they feed has to hold everywhere.
func extractLimitFor(avail int64) int64 {
	if avail <= 0 {
		return 10 << 20 // unknown machine: the conservative default
	}
	limit := avail / 100
	if limit < 5<<20 {
		return 5 << 20
	}
	if limit > 50<<20 {
		return 50 << 20
	}
	return limit
}

// extractionSlots is how many PDFs may be parsed at the same time. This is the
// axis that gets forgotten: one 15 MB extraction is harmless, four at once are
// not, and any per-file limit is worthless while ten uploads run in parallel.
// Queueing costs a little waiting; not queueing costs the server.
func extractionSlots() int { return extractionSlotsFor(availableMemory()) }

func extractionSlotsFor(avail int64) int {
	switch {
	case avail <= 0 || avail < 4<<30:
		return 1
	case avail < 12<<30:
		return 2
	default:
		return 3
	}
}
