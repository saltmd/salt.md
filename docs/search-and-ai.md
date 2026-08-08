# Concept: search with context, locally

The goal: not just search for words but for meaning — "where does it say
anything about notice periods?" should also find pages in which the word
"notice period" never appears. With no outward network, no GPU, no MCP, inside
the same single binary.

This paper describes what that takes, in which order, and where the traps are.
It is a plan, not a decision.

---

## 1. The rule that is not negotiable

Today's search index (`pages_fts`) holds **every page of the whole instance**
and knows nothing about permissions. That nobody else's page turns up in it is
down to the query alone — and that filters in **two** stages:

1. `WHERE p.workspace_id IN (…)` — only workspaces the person is a member of
   (plus a running break-glass access), narrowed by the token scope
2. `canRead` per hit — this catches **other people's private pages inside** a
   workspace you are allowed to enter

Every new search has to have both stages. The second one gets forgotten because
it feels like a special case; it is the more common one.

**A vector is content, not a metadata row.** What a model computes from a page
gives away its subject. Embeddings therefore belong in the same protection class
as `pages.body`:

- delete with the page (foreign key, the way `page_revisions` does)
- delete with the workspace (add to `purgeWorkspace`, the way `pages_fts` is)
- in an export, hand out only what the exporting person may see anyway
- never a "global" similarity endpoint without a workspace filter

---

## 2. What is already weak today

Before any model joins, three things about what exists:

**The truncation bug.** `LIMIT 40` sits **before** the `canRead` filter, and it
stops at 20 hits. Anybody searching in a workspace with many private pages of
other people silently gets too few results. The fix is the one from the audit
log: fetch more until enough is left, or overshoot generously.

**No stemming, no umlaut folding.** `pages_fts` runs with the default
tokenizer. "Verträge" does not find "Vertrag", "Strasse" does not find "Straße"
(the German examples are the point: that is where the inflection lives). FTS5
can do this (`tokenize = "unicode61 remove_diacritics 2"`, plus a dictionary or
Porter for English) — it costs no more than a rebuild of the index.

**Prefix hits only.** The query appends a `*` to every term. That is good for
typing in the search box and bad for whole sentences.

Fixing these three is worth noticeably more than the first model — and they keep
holding once the model arrives.

---

## 3. What the agent repo has already solved

In the neighbouring project (`~/Code/agent`, Node/TypeScript) exactly this
search is already running — there for skills and for memory. It has nothing to
do with salt.md and will not become part of it: it is a source of decisions that
were once taken there and proven in use. The code is not transferable anyway
(Node against Go without CGO); the decisions are:

**No vector index at this size.** The comment in `src/skills/semantic.ts` says
it in as many words: at homelab scale you need no vector database — embed,
cache in SQLite, raw cosine over the small set. The same arithmetic as in
section 7. pgvector sits ready behind the same interface, should it ever be
needed.

**Cache on a content hash.** Only what changed is recomputed; for an appended
paragraph, keeping up costs one paragraph, not the page.

**Cut along the structure, not by character count.** There it cuts per
conversation turn, so a hit points at the actual exchange. For us that means:
cut at block boundaries, not every 700 characters mid-sentence.

**A cascade instead of a switch.** `semantic → FTS5 → substring`. If the model
is missing, the cache is cold or the embedding fails, the search drops one level
— it never breaks.

**A signature over the cache.** `embedderSignature()` remembers which model
produced the vectors. Vectors from different models live in different spaces and
must never be compared with one another. That is exactly what the `model` column
in the data model below is for.

**Threads pinned.** The built-in embedder sets `intraOpNumThreads` to 1 — it
caps the load on a small box and works around an LXC container not being allowed
to set CPU affinity. That concerns our Proxmox container directly.

**A time window.** Only the last 120 days are embedded, older material stays
with the full-text search. A way of keeping the set small that we should note —
for us it would be more like "only pages that are not archived".

And the difference that decides everything: **the agent has exactly one user.**
There is no permission check there because there does not have to be one.
Whoever adopts these building blocks without putting in the two filters from
section 1 builds in the very leak this paper is meant to prevent.

---

## 4. Staged plan

### Stage 0 — sharpen the full-text search

Truncation bug, umlaut folding, stemming. No new data model, no model, no new
dependencies. One afternoon.

### Stage 1 — the path, still without a model

Create the passage table, fill it on save, run the search over it — at first
with the full text only. That puts the whole scaffolding in place: the cutting,
the keeping-up on write, the deleting along with, and above all the permission
check from section 1. All of it can be tested before a single number comes out
of a model.

This is the stage you do not skip. Whoever starts with the model builds the
permission check as a side issue and does not notice mistakes in it, because the
results look plausible enough.

### Stage 2 — the small model, in the program

No service next door, no endpoint, no address to enter: the model runs in the
same program on the CPU. That is the point of the whole exercise — an instance
should be able to do everything out of the box, even with no outward network.

In Go without CGO there are two serious routes for that, plus a cheaper
approximation:

| Route | Size | Quality | Effort |
|---|---|---|---|
| **Static word vectors** (weighted mean, SIF) | 20–40 MB | decent for "related in subject", blind to word order and negation | pure Go, a few dozen lines of arithmetic |
| **wazero + model as WASM** | 30–120 MB | real sentence meaning | pure Go runtime, but the model has to be converted and wired up |
| **spago** (pure Go, BERT family) | same | same | fits the MiniLM architecture; measure maturity and speed first |

What is ruled out: `onnxruntime` bindings (CGO, buries the single-binary
promise), a sidecar process (one binary becomes a pair) and embedding in the
browser (every phone downloads 100 MB).

**Order within the stage:** static vectors first. They are built in a day, cost
microseconds per page and give a yardstick. If the search is good enough with
them, the matter is settled. If it is not, at least you know what you are
measuring against when prototyping wazero and spago.

**Where the model comes from.** Putting 120 MB into a 24 MB binary is too much.
Three options: bake in a quantised build (~30–40 MB) with `go:embed`, download
it once on first use (which breaks "runs without a network"), or put it in the
Docker image and leave it out of the binary. I would bake it in if the quantised
build is good enough — otherwise the promise of the one file is only half true.

**Model choice** if it comes to that: `paraphrase-multilingual-MiniLM-L12-v2`,
384 dimensions. Multilingual, so German pages embed properly, small enough for
the CPU — and proven on real material in the neighbouring project.

### Rejected: an endpoint to configure

The obvious move would be to hand the embedding off to an OpenAI-compatible
service (Ollama and friends). That would be the least work and is still wrong
for this project: it turns "one file, done" into "one file plus a service you
have to run yourself". Anybody who wants that has the full-text search and does
not need us. Written down so the question is not asked twice.

---

## 5. Data model

A page is too big for one vector. It is cut into passages of roughly 500–800
characters along the block boundaries, with a little overlap.

```sql
CREATE TABLE page_chunks (
  id           TEXT PRIMARY KEY,
  page_id      TEXT NOT NULL REFERENCES pages(id) ON DELETE CASCADE,
  workspace_id TEXT NOT NULL,   -- denormalised: the filter runs BEFORE the search
  ord          INTEGER NOT NULL,
  text         TEXT NOT NULL,   -- for the excerpt in the result
  vec          BLOB NOT NULL,   -- int8 quantised, length = dimensions
  model        TEXT NOT NULL,   -- name+version; foreign rows are ignored
  updated_at   TEXT NOT NULL
);
CREATE INDEX idx_chunk_ws ON page_chunks(workspace_id);
```

`workspace_id` is deliberately duplicated into the table: that way the set can
be narrowed **before** the arithmetic instead of thrown away afterwards.

`model` allows a change without a migration: write new rows under a new name,
remove the old ones during clean-up, and let the query take only the current
one.

---

## 6. The query path

```
Question
  └─ vector of the question (the same method as when indexing)
       └─ determine the allowed workspaces  (membership + break-glass + token scope)
            └─ candidates: page_chunks WHERE workspace_id IN (…) AND model = ?
                 └─ cosine against all candidates, best N per page
                      └─ canRead per page      ← the stage that tends to be missing
                           └─ merge with the BM25 hits (RRF), return 20
```

**Filter first, then compute.** The other way round ("the 20 most similar, then
check permissions") regularly returns nothing at all to people with few read
permissions, even though matching pages exist — the same mistake as the
truncation bug in stage 0, only harder to notice.

**Merge rather than replace.** Pure meaning-based search is bad at proper names,
file references and customer numbers — that is, exactly what people search for
at work. Ranking BM25 and cosine separately and merging the ranks (reciprocal
rank fusion, three lines) beats either on its own.

---

## 7. What it costs

Reckoned with 384 dimensions, int8 (384 bytes per passage):

| Holding | Passages | Storage | Cosine over all |
|---|---|---|---|
| the instance today (805 pages) | ~2,500 | ~1 MB | < 1 ms |
| medium company (10,000 pages) | ~35,000 | ~13 MB | ~5 ms |
| large (100,000 pages) | ~350,000 | ~130 MB | ~50 ms |

**From which follows the most important thing in this paper: for the time being
no vector index is needed.** A linear pass over the permitted passages is fast
enough well into six figures and carries none of the maintenance costs of HNSW
and friends — no rebuild, no parameters, no silent misses. The decision to build
a real index thereby moves out to a problem these instances will probably never
have.

Indexing costs once: at stage 1, seconds for the whole holding; at stage 2,
about 10–30 ms per passage on one container core, so roughly a minute for 2,500
passages — in the background, with the same queue as the bulk import.

---

## 8. Lifecycle

| Event | What happens |
|---|---|
| page saved | recompute the passages, the way `reindexPage` does today |
| page moved to the trash | keep the passages but take them out of the search (`trashed_at`) |
| page deleted for good | falls away by foreign key |
| workspace deleted | delete it in `purgeWorkspace` too — otherwise a shadow of the deleted area stays behind, from which its subjects can be reconstructed |
| model changed | new rows with a new `model`, old ones removed in the background |
| export / backup | vectors are content: only with the pages that may travel |

---

## 9. What I would not do

- **No "similar pages" endpoint without a permission check.** It looks harmless
  and is the most convenient way to walk past the workspace filter.
- **No full text to a foreign model.** The moment a key for a third-party API is
  in play, the promise of the instance no longer holds — and no setting says
  that it no longer holds.
- **No vectors in a plain-text export**, as long as it is unclear who opens it.
- **No ANN index before the linear search really is too slow.** See section 7;
  that will take a long time.
- **No model in the same process without a memory limit.** A container with
  512 MB is the normal case in self-hosting.

---

## 10. Order

1. **Stage 0** — truncation bug, umlauts, stemming. Straight away, independent
   of the rest. It works even if a model never arrives.
2. **Stage 1** — passage table and indexing, at first with the full text only.
   The path stands and the permission check is proven before vectors are in
   play.
3. **Stage 2a** — static word vectors, merged with BM25. Measure on our own
   material whether that is enough.
4. **Stage 2b** only if 2a is demonstrably too weak — then wazero or spago,
   measured against each other first.

The jump from 1 to 2 is small if 1 is built properly: one column is added, and
one function that fills it.
