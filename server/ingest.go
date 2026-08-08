package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Bulk import, without the content passing through the agent.
//
// The problem being solved here: to create 654 Trello cards, an agent used to
// have to write every character itself — once reading the source, once writing
// it through create_page. At ~1.5 million characters of card content every
// agent breaks off along the way, however skilful it is. That is not a question
// of care but a hard limit of its context window.
//
// The reversal: the agent names only the SOURCE and the MAPPING (a few hundred
// characters), Salt fetches the data itself and creates the pages. The import
// is then independent of the size of the source and succeeds even for a weak
// agent — all it has to do is start a job and ask how far along it is.
//
// The security boundary: a tool that lets the server fetch arbitrary URLs is a
// classic SSRF hole. Salt sits in a private network and could reach neighbours
// through it that are unreachable from outside — routers, hypervisors, cloud
// metadata services. That is why safeDial checks EVERY resolved address and
// dials exactly the one it checked (see there).

const (
	ingestMaxBytes = 64 << 20 // upper limit for the fetched source
	ingestMaxItems = 20000    // ripcord against endless sources
	ingestKeepJobs = 20       // this many finished jobs stay retrievable
)

// --- Auftragsverwaltung ------------------------------------------------------

type ingestJob struct {
	ID       string   `json:"job_id"`
	Status   string   `json:"status"` // running | done | failed
	Total    int      `json:"total"`
	Created  int      `json:"created"`
	Failed   int      `json:"failed"`
	Errors   []string `json:"errors,omitempty"`
	Note     string   `json:"note,omitempty"`
	Target   string   `json:"target"`
	Started  string   `json:"started_at"`
	Finished string   `json:"finished_at,omitempty"`
	// OwnerID: the registry is process wide and the jobs carry target details
	// and row titles. Without an owner, anybody holding a job id could read the
	// progress of somebody else's import.
	OwnerID string `json:"-"`
}

type ingestRegistry struct {
	mu    sync.Mutex
	jobs  map[string]*ingestJob
	order []string
}

func newIngestRegistry() *ingestRegistry {
	return &ingestRegistry{jobs: map[string]*ingestJob{}}
}

func (reg *ingestRegistry) add(j *ingestJob) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.jobs[j.ID] = j
	reg.order = append(reg.order, j.ID)
	// Keep only the last N — the jobs live in memory, not in the database. A
	// restart loses the STATUS, not the work: pages already created are saved.
	for len(reg.order) > ingestKeepJobs {
		delete(reg.jobs, reg.order[0])
		reg.order = reg.order[1:]
	}
}

func (reg *ingestRegistry) get(id string) (ingestJob, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	j, ok := reg.jobs[id]
	if !ok {
		return ingestJob{}, false
	}
	return *j, true // a copy: the caller must not reach into the running job
}

func (reg *ingestRegistry) update(id string, fn func(*ingestJob)) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if j, ok := reg.jobs[id]; ok {
		fn(j)
	}
}

// --- Fetching the source -----------------------------------------------------

// allowPrivateImport opens imports up to private networks. Deliberately ONLY
// through an environment variable at startup (SALT_IMPORT_ALLOW_PRIVATE=1), not
// through the API and certainly not through MCP: whoever starts the service
// makes that decision — an agent cannot. Meant for self-hosted sources on your
// own network (your own Jira, your own wiki).
var allowPrivateImport = os.Getenv("SALT_IMPORT_ALLOW_PRIVATE") == "1"

// blockedIP decides whether an address is off limits for the server. Everything
// that is not publicly routable is refused: loopback, private networks,
// link-local (which is where 169.254.169.254 lives, the metadata service of
// many providers), multicast and the unspecified address.
func blockedIP(ip net.IP) bool {
	if allowPrivateImport {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified()
}

// safeDial resolves the name, checks EVERY address and then connects to
// exactly the address it checked.
//
// The second half is the important one: connecting through the name again after
// the check would let an attacker serve a different address between check and
// connection (DNS rebinding), and the check would be worthless. Because the
// dialer runs again on EVERY redirect, this covers redirects to internal
// targets as well.
func safeDial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve %q", host)
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	for _, ip := range ips {
		if blockedIP(ip) {
			return nil, fmt.Errorf("refusing to fetch from %s (%s): only public addresses are allowed, "+
				"so an import cannot be used to reach this server's private network", host, ip)
		}
	}
	var lastErr error
	for _, ip := range ips {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func ingestHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   3 * time.Minute,
		Transport: &http.Transport{DialContext: safeDial},
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
				return fmt.Errorf("refusing to follow a %s:// redirect", r.URL.Scheme)
			}
			return nil
		},
	}
}

// fetchSource fetches the source. headers allows authentication (bearer token,
// API key) without Salt storing the credentials — they hold for this one fetch
// only.
func fetchSource(rawURL string, headers map[string]string) ([]byte, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return nil, fmt.Errorf("url must start with http:// or https://")
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid url: %v", err)
	}
	req.Header.Set("User-Agent", "salt.md/"+Version+" (import)")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := ingestHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch the url: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, ingestMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("could not read the response: %v", err)
	}
	if len(body) > ingestMaxBytes {
		return nil, fmt.Errorf("the source is larger than %d MB", ingestMaxBytes>>20)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(body))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return nil, fmt.Errorf("the source answered %s: %s", resp.Status, snippet)
	}
	return body, nil
}

// --- Feldzuordnung -----------------------------------------------------------

// jsonPath reads a value through a path like "name", "card.due" or
// "labels[].name" (the last one picks the field out of every element of a
// list). Deliberately kept small: that covers the shape REST answers almost
// always have, without dragging a whole query language along.
func jsonPath(v any, path string) any {
	if path == "" || v == nil {
		return v
	}
	seg, rest, _ := strings.Cut(path, ".")
	pluck := strings.HasSuffix(seg, "[]")
	seg = strings.TrimSuffix(seg, "[]")

	cur := v
	if seg != "" {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[seg]
	}
	if pluck {
		arr, ok := cur.([]any)
		if !ok {
			return nil
		}
		out := []any{}
		for _, e := range arr {
			if rest == "" {
				out = append(out, e)
			} else if x := jsonPath(e, rest); x != nil {
				out = append(out, x)
			}
		}
		return out
	}
	if rest == "" {
		return cur
	}
	return jsonPath(cur, rest)
}

// scalarString turns a JSON value into text for titles and text fields.
func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			if s := scalarString(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return ""
}

// ingestResolve describes a link inside the same answer: an element carries a
// foreign id while the plain text sits in another list. That is exactly the
// shape of Trello (card → idList → lists[].name), Jira, Airtable and Asana.
// Without it the column would hold a meaningless id.
type ingestResolve struct {
	From  string `json:"from"`  // path to the lookup list, e.g. "lists"
	Match string `json:"match"` // field in it that matches the id, e.g. "id"
	To    string `json:"to"`    // field whose value is put in, e.g. "name"
}

type ingestSpec struct {
	URL        string                   `json:"url"`
	Headers    map[string]string        `json:"headers"`
	Items      string                   `json:"items"`
	Title      string                   `json:"title"`
	Markdown   string                   `json:"markdown"`
	Properties map[string]string        `json:"properties"`
	Resolve    map[string]ingestResolve `json:"resolve"`
	DatabaseID string                   `json:"database_id"`
	ParentID   string                   `json:"parent_id"`
	WorkspaceI string                   `json:"workspace_id"`
	Limit      int                      `json:"limit"`
}

// buildResolvers builds dictionaries id → plain text from the lookup lists.
func buildResolvers(doc any, spec ingestSpec) map[string]map[string]string {
	out := map[string]map[string]string{}
	for field, r := range spec.Resolve {
		arr, ok := jsonPath(doc, r.From).([]any)
		if !ok {
			continue
		}
		table := map[string]string{}
		for _, e := range arr {
			k := scalarString(jsonPath(e, r.Match))
			v := scalarString(jsonPath(e, r.To))
			if k != "" && v != "" {
				table[k] = v
			}
		}
		out[field] = table
	}
	return out
}

// applyResolve ersetzt Ids durch Klartext — auch innerhalb von Listen.
func applyResolve(v any, table map[string]string) any {
	if table == nil {
		return v
	}
	if arr, ok := v.([]any); ok {
		out := make([]any, 0, len(arr))
		for _, e := range arr {
			out = append(out, applyResolve(e, table))
		}
		return out
	}
	if s := scalarString(v); s != "" {
		if mapped, ok := table[s]; ok {
			return mapped
		}
	}
	return v
}

// --- Carrying it out ---------------------------------------------------------

type ingestItem struct {
	title string
	md    string
	props map[string]any
}

// planIngest fetches the source and shapes it into entries — without writing
// anything. A wrongly mapped import therefore fails BEFORE half-created pages
// are lying around.
func planIngest(spec ingestSpec) ([]ingestItem, error) {
	body, err := fetchSource(spec.URL, spec.Headers)
	if err != nil {
		return nil, err
	}
	var doc any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("the source is not valid JSON: %v", err)
	}
	return mapItems(doc, spec)
}

// mapItems shapes a fetched document into entries. Separate from planIngest so
// the mapping is testable without network access — it is the part the mistakes
// live in.
func mapItems(doc any, spec ingestSpec) ([]ingestItem, error) {
	raw := doc
	if spec.Items != "" {
		raw = jsonPath(doc, spec.Items)
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("items path %q does not point at a list — pass the path to the array of records (for example \"cards\"), or omit it if the response is a list itself", spec.Items)
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("the source contains no records at %q", spec.Items)
	}
	if len(arr) > ingestMaxItems {
		return nil, fmt.Errorf("the source has %d records, more than the limit of %d", len(arr), ingestMaxItems)
	}
	limit := spec.Limit
	if limit > 0 && limit < len(arr) {
		arr = arr[:limit]
	}
	resolvers := buildResolvers(doc, spec)

	items := make([]ingestItem, 0, len(arr))
	for i, e := range arr {
		title := strings.TrimSpace(scalarString(jsonPath(e, spec.Title)))
		if title == "" {
			title = fmt.Sprintf("Untitled %d", i+1)
		}
		if len([]rune(title)) > maxTitleLen {
			title = string([]rune(title)[:maxTitleLen])
		}
		it := ingestItem{title: title, props: map[string]any{}}
		if spec.Markdown != "" {
			it.md = scalarString(jsonPath(e, spec.Markdown))
		}
		for prop, path := range spec.Properties {
			v := jsonPath(e, path)
			// The mapping names the SOURCE PATH; the lookup goes through its last
			// segment, so that "idList" and "card.idList" behave the same.
			key := path
			if i := strings.LastIndexByte(key, '.'); i >= 0 {
				key = key[i+1:]
			}
			v = applyResolve(v, resolvers[strings.TrimSuffix(key, "[]")])
			if v != nil {
				it.props[prop] = v
			}
		}
		items = append(items, it)
	}
	return items, nil
}

// ensureIngestOptions creates the missing select options — ONCE for the whole
// import, not per row.
//
// Without it the import is unusable for a weak agent: the 11 Trello lists are
// not options in the Salt schema to begin with, every row would get an empty
// status, and the agent would have to notice the gap itself. The import knows
// better than it does — so the import does it.
func (s *Server) ensureIngestOptions(dbID string, items []ingestItem, nameToID map[string]string) (int, error) {
	schema, views, err := s.loadCollection(dbID)
	if err != nil {
		return 0, err
	}
	// Collect the wanted values per select property, keeping the order stable.
	want := map[string][]string{}
	seen := map[string]bool{}
	for _, it := range items {
		for prop, v := range it.props {
			id := nameToID[strings.ToLower(prop)]
			if id == "" {
				continue
			}
			for _, val := range valueStrings(v) {
				k := id + "\x00" + strings.ToLower(val)
				if !seen[k] {
					seen[k] = true
					want[id] = append(want[id], val)
				}
			}
		}
	}
	added := 0
	for i, p := range schema {
		id, _ := p["id"].(string)
		typ, _ := p["type"].(string)
		if typ != "select" && typ != "multiselect" {
			continue
		}
		opts, _ := p["options"].([]any)
		have := map[string]bool{}
		taken := map[string]bool{}
		for _, o := range opts {
			om, _ := o.(map[string]any)
			if n, _ := om["name"].(string); n != "" {
				have[strings.ToLower(n)] = true
			}
			if oid, _ := om["id"].(string); oid != "" {
				taken[oid] = true
			}
		}
		for _, val := range want[id] {
			if have[strings.ToLower(val)] {
				continue
			}
			// Give it a colour right away: an option without one shows in the
			// board as a colourless header, and a kanban lives on telling the
			// columns apart by hue. Taken in turn from optionPalette
			// (import_csv.go) — the same source as the CSV import, so that no
			// second truth appears.
			opts = append(opts, map[string]any{
				"id":    slugID(val, taken),
				"name":  val,
				"color": optionPalette[added%len(optionPalette)],
			})
			have[strings.ToLower(val)] = true
			added++
		}
		schema[i]["options"] = opts
	}
	if added > 0 {
		if err := s.saveCollection(dbID, schema, views); err != nil {
			return 0, err
		}
	}
	return added, nil
}

// valueStrings breaks a mapped value into individual select values.
func valueStrings(v any) []string {
	if arr, ok := v.([]any); ok {
		out := []string{}
		for _, e := range arr {
			if s := strings.TrimSpace(scalarString(e)); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	if s := strings.TrimSpace(scalarString(v)); s != "" {
		return []string{s}
	}
	return nil
}

// startIngest checks target and mapping, fetches the source and starts the job
// in the background. The caller gets a job id back straight away — an import
// with hundreds of entries would otherwise run into every timeout there is.
func (s *Server) startIngest(u *user, spec ingestSpec) (string, error) {
	if strings.TrimSpace(spec.URL) == "" {
		return "", fmt.Errorf("url is required")
	}
	if spec.Title == "" {
		return "", fmt.Errorf("title is required — name the field each record's title comes from (for example \"name\")")
	}

	// Work out the target and check write permission BEFORE anything is fetched.
	var parentID, workspaceID, target string
	var nameToID map[string]string
	if spec.DatabaseID != "" {
		// tokenCanReach on top: canWrite does not know the workspace boundary of a
		// restricted token, which would otherwise write outside its own area.
		if !s.canWrite(u.ID, spec.DatabaseID) || u.TokenScope == "read" || !s.credentialMayEnter(u, s.pageWorkspace(spec.DatabaseID)) {
			return "", fmt.Errorf("database %q not found", spec.DatabaseID)
		}
		schema, _, err := s.loadCollection(spec.DatabaseID)
		if err != nil {
			return "", err
		}
		if err := s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ? AND trashed_at IS NULL`,
			spec.DatabaseID).Scan(&workspaceID); err != nil {
			return "", fmt.Errorf("database %q not found", spec.DatabaseID)
		}
		nameToID = map[string]string{}
		known := []string{}
		for _, p := range schema {
			id, _ := p["id"].(string)
			name, _ := p["name"].(string)
			if id != "" {
				nameToID[strings.ToLower(id)] = id
				known = append(known, name)
			}
			if name != "" {
				nameToID[strings.ToLower(name)] = id
			}
		}
		// A misspelled column may not quietly run into nothing.
		for prop := range spec.Properties {
			if nameToID[strings.ToLower(prop)] == "" {
				return "", fmt.Errorf("the database has no property %q — it has: %s (call get_schema, or add it with update_schema first)",
					prop, strings.Join(known, ", "))
			}
		}
		parentID = spec.DatabaseID
		target = "database " + spec.DatabaseID
	} else if spec.ParentID != "" {
		if !s.canWrite(u.ID, spec.ParentID) || u.TokenScope == "read" || !s.credentialMayEnter(u, s.pageWorkspace(spec.ParentID)) {
			return "", fmt.Errorf("parent page %q not found", spec.ParentID)
		}
		if err := s.db.QueryRow(`SELECT workspace_id FROM pages WHERE id = ? AND trashed_at IS NULL`,
			spec.ParentID).Scan(&workspaceID); err != nil {
			return "", fmt.Errorf("parent page %q not found", spec.ParentID)
		}
		parentID = spec.ParentID
		target = "pages under " + spec.ParentID
	} else {
		ws, err := s.mcpCreateWorkspaceTarget(u, spec.WorkspaceI)
		if err != nil {
			return "", err
		}
		workspaceID = ws
		target = "top-level pages in workspace " + ws
	}

	// Fetching and mapping happen BEFORE the background job: a typo in the path or
	// an unreachable source should come back as an error immediately, not only when
	// somebody asks.
	items, err := planIngest(spec)
	if err != nil {
		return "", err
	}

	note := ""
	if spec.DatabaseID != "" {
		added, err := s.ensureIngestOptions(spec.DatabaseID, items, nameToID)
		if err != nil {
			return "", err
		}
		if added > 0 {
			note = fmt.Sprintf("added %d missing select option(s) from the source", added)
		}
	}

	job := &ingestJob{
		ID: newID(), Status: "running", Total: len(items),
		Target: target, Started: now(), Note: note, OwnerID: u.ID,
	}
	s.ingest.add(job)
	go s.runIngest(job.ID, u.ID, parentID, workspaceID, items, nameToID)
	return job.ID, nil
}

// runIngest creates the entries. Runs in the background and keeps writing the
// progress into the job, so an agent can watch.
func (s *Server) runIngest(jobID, userID, parentID, workspaceID string, items []ingestItem, nameToID map[string]string) {
	var pos float64
	if parentID != "" {
		s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM pages WHERE parent_id = ?`, parentID).Scan(&pos)
	} else {
		s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM pages WHERE parent_id IS NULL AND workspace_id = ?`, workspaceID).Scan(&pos)
	}
	var parent any
	if parentID != "" {
		parent = parentID
	}

	for _, it := range items {
		content := "[]"
		if it.md != "" {
			if c, err := mdToBlocksJSON(it.md); err == nil {
				content = c
			}
		}
		id := newID()
		ts := now()
		_, err := s.db.Exec(`INSERT INTO pages (id, parent_id, title, content, position, created_at, updated_at, workspace_id, owner_id, visibility)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'workspace')`,
			id, parent, it.title, content, pos, ts, ts, workspaceID, userID)
		if err != nil {
			s.ingest.update(jobID, func(j *ingestJob) {
				j.Failed++
				if len(j.Errors) < 10 {
					j.Errors = append(j.Errors, fmt.Sprintf("%s: %v", it.title, err))
				}
			})
			continue
		}
		pos++
		if len(it.props) > 0 && nameToID != nil {
			patch := map[string]any{}
			for prop, v := range it.props {
				if pid := nameToID[strings.ToLower(prop)]; pid != "" {
					patch[pid] = v
				}
			}
			if len(patch) > 0 {
				b, _ := json.Marshal(patch)
				if _, err := s.mcpSetProperties(id, b); err != nil {
					s.ingest.update(jobID, func(j *ingestJob) {
						if len(j.Errors) < 10 {
							j.Errors = append(j.Errors, fmt.Sprintf("%s: properties: %v", it.title, err))
						}
					})
				}
			}
		}
		s.reindexPage(id)
		s.ingest.update(jobID, func(j *ingestJob) { j.Created++ })
	}

	s.ingest.update(jobID, func(j *ingestJob) {
		j.Status = "done"
		j.Finished = now()
	})
	s.pagesChanged()
}
