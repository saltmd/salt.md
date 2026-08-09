package server

// The cover presets the interface offers (W113).
//
// Why this list exists in Go at all, when the picker lives in Editor.tsx: an
// agent cannot see the picker. It sees the MCP tool schema and nothing else, so
// unless the presets are reachable over MCP, every agent invents its own
// gradient — or, far more likely, sets no cover at all, which is what three
// different agents did before anybody noticed.
//
// Two sources of truth is the cost, and it is paid for by TestCoverPresetsMatchUI,
// which reads Editor.tsx and fails if the two lists drift apart. A picker that
// offers eighteen gradients while agents are told about twelve is worse than
// either on its own.
var coverPresets = []string{
	// The original six, one per user colour.
	"gradient:linear-gradient(120deg,#4fa872,#2f7d4f)",
	"gradient:linear-gradient(120deg,#6aa9e0,#3b6fb5)",
	"gradient:linear-gradient(120deg,#e0c56a,#b58a3b)",
	"gradient:linear-gradient(120deg,#b07de0,#7d4fb0)",
	"gradient:linear-gradient(120deg,#e0846a,#c4554d)",
	"gradient:linear-gradient(120deg,#6ad0d0,#3ba0a8)",
	// W96: softer two- and three-tone blends, light to dark, so a page emoji
	// sitting on the left stays legible.
	"gradient:linear-gradient(120deg,#ffd3a5,#fd6585)",
	"gradient:linear-gradient(120deg,#a8edea,#5b86e5)",
	"gradient:linear-gradient(120deg,#f6d365,#fda085)",
	"gradient:linear-gradient(120deg,#d4fc79,#4a934a)",
	"gradient:linear-gradient(120deg,#e0c3fc,#8e63c9)",
	"gradient:linear-gradient(120deg,#f5efe6,#b8a389)",
	"gradient:linear-gradient(120deg,#fbc2eb,#a18cd1)",
	"gradient:linear-gradient(120deg,#fddb92,#d1858c)",
	"gradient:linear-gradient(120deg,#9be2d5,#2c7a7b)",
	"gradient:linear-gradient(120deg,#c9d6ff,#5c6bc0)",
	"gradient:linear-gradient(135deg,#ffecd2,#fcb69f 55%,#e0846a)",
	"gradient:linear-gradient(135deg,#a1c4fd,#c2e9fb 45%,#6aa9e0)",
	// Saturated, and on purpose: everything above is a soft blend, so a page
	// that wants to shout had nothing to reach for. These are the site's own
	// beam colours — the full arc, and the green-to-violet short version of it.
	"gradient:linear-gradient(120deg,#ff2d60,#ff8a2d 25%,#ffd12d 45%,#22c55e 62%,#3b82f6 80%,#9333ea)",
	"gradient:linear-gradient(120deg,#22c55e,#3b82f6 55%,#9333ea)",
	"gradient:linear-gradient(120deg,#ffd12d,#ff8a2d 45%,#ff2d60)",
	"gradient:linear-gradient(120deg,#22d3ee,#3b82f6 50%,#9333ea)",
}

// coverHint is the one sentence that turns "cover" from a field an agent skips
// into one it can actually fill. It goes in every tool description that takes a
// cover, because the schema is the only documentation an agent reliably reads —
// neither llms.txt nor the website carries the format.
const coverHint = `A page cover. Two forms only: ` +
	`"gradient:linear-gradient(120deg,#a8edea,#5b86e5)" or an uploaded path ` +
	`like "/files/abc123.jpg". An external image URL is refused, because every ` +
	`viewer of the page would then fetch it from that host. ` +
	`Call list with kind="cover_presets" for the gradients the interface itself offers.`
