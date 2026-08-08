package server

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Account lifecycle (W105).
//
// There used to be only "delete", and it tidied up silently: memberships went
// by CASCADE, and if somebody was a workspace's only member, that workspace
// stayed behind with zero of them — invisible to everyone, but still holding
// its pages, files and search index entries. The "last admin" guard only fired
// when leaving a workspace, never when deleting the account.
//
// Three things change that:
//
//	Deactivate       the normal case when somebody leaves. Sign-in closed,
//	                 sessions and tokens ended, everything still attributable.
//	Show the impact  whoever deletes sees first what hangs off the account.
//	Hand over        shared workspaces go to the longest-serving remaining
//	                 admin, failing that to the owner. Never into nothing.
//
// The personal space goes with the person: passing it quietly to the boss is
// exactly what the permission model exists to prevent.

// purgeWorkspace deletes a workspace and its content.
//
// `pages.workspace_id` was added later via ensureColumn and has NO foreign
// key — only workspace_members, break_glass and tag_colors cascade off
// `workspaces`. A bare `DELETE FROM workspaces` therefore left the pages
// standing: invisible to every interface (the membership it checks against is
// gone) but still in the database — and any public share link kept working,
// with nobody left who could revoke it.
//
// All in one transaction, so no half-state can survive.
func (s *Server) purgeWorkspace(wsID string) error {
	// Note first which uploads hang off these pages — after the DELETE nothing
	// records it any more. A file under /files/<name> is fetchable by any
	// signed-in account; left behind, the contents of a deleted personal space
	// stayed reachable to anyone holding the URL (from an export, a copy, their
	// browser history).
	refs := map[string]bool{}
	var pageIDs []string
	if rows, err := s.db.Query(`SELECT id, COALESCE(content,''), COALESCE(props,''), COALESCE(cover,'') FROM pages WHERE workspace_id = ?`, wsID); err == nil {
		for rows.Next() {
			var id, content, props, cover string
			if rows.Scan(&id, &content, &props, &cover) != nil {
				continue
			}
			pageIDs = append(pageIDs, id)
			for _, m := range fileRefPattern.FindAllStringSubmatch(content+"\n"+props+"\n"+cover, -1) {
				refs[m[1]] = true
			}
		}
		rows.Close() // drain first, then carry on (one DB connection)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Share links and comments hang off pages and go with them; pages_fts is a
	// virtual table without foreign keys, and invites carry the workspace id
	// without a reference — both by hand.
	if _, err := tx.Exec(`DELETE FROM pages_fts WHERE id IN (SELECT id FROM pages WHERE workspace_id = ?)`, wsID); err != nil {
		return err
	}
	// chunks_fts by hand as well: virtual tables know no cascade. page_chunks
	// itself hangs off a foreign key and falls with the pages.
	if _, err := tx.Exec(`DELETE FROM chunks_fts WHERE chunk_id IN
		(SELECT id FROM page_chunks WHERE workspace_id = ?)`, wsID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM page_chunks WHERE workspace_id = ?`, wsID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM pages WHERE workspace_id = ?`, wsID); err != nil {
		return err
	}
	tx.Exec(`DELETE FROM invites WHERE workspace_id = ?`, wsID)
	if _, err := tx.Exec(`DELETE FROM workspaces WHERE id = ?`, wsID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Open editors: anyone with the page in front of them would otherwise keep
	// typing into nothing — the yjs writes fail on the foreign key, the error is
	// only logged, and the person notices nothing until they reload.
	for _, pid := range pageIDs {
		s.collab.reset(pid)
	}
	s.removeUnreferencedFiles(refs)
	s.pagesChanged()
	return nil
}

// removeUnreferencedFiles deletes uploads no page points at any more.
//
// Only AFTER the commit: while the rows are still there, every file would look
// referenced. And only when truly nothing points at it — the same file can live
// on in a copy or a moved page, and a missing image in somebody else's
// workspace would be worse than a leftover on disk.
func (s *Server) removeUnreferencedFiles(refs map[string]bool) {
	for name := range refs {
		// No path component: the name comes from page content and is therefore
		// under the user's influence. filepath.Base alone is not enough against
		// "..", hence the outright rejection as well.
		if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
			continue
		}
		like := "%/files/" + name + "%"
		var used int
		s.db.QueryRow(`SELECT COUNT(*) FROM pages
			WHERE content LIKE ? OR props LIKE ? OR cover LIKE ?`, like, like, like).Scan(&used)
		if used > 0 {
			continue
		}
		// The CURRENT content is not the only thing that counts. A revision can
		// be restored (history.go writes it back verbatim), a comment can link
		// to the file, and a profile picture lives in the same directory.
		// Without these three queries the clean-up left a dead image inside a
		// restorable version of SOMEBODY ELSE'S page.
		var elsewhere int
		s.db.QueryRow(`SELECT
			(SELECT COUNT(*) FROM page_revisions WHERE content LIKE ?) +
			(SELECT COUNT(*) FROM comments WHERE body LIKE ?) +
			(SELECT COUNT(*) FROM workspaces WHERE image LIKE ?) +
			(SELECT COUNT(*) FROM users WHERE avatar LIKE ?)`,
			like, like, like, like).Scan(&elsewhere)
		if elsewhere > 0 {
			continue
		}
		if err := os.Remove(filepath.Join(s.dataDir, "files", name)); err != nil && !os.IsNotExist(err) {
			log.Printf("remove upload %s: %v", name, err)
		}
	}
}

// deletionImpact describes what deleting an account drags along with it.
type deletionImpact struct {
	UserName string `json:"userName"`
	// Personal: spaces that disappear along with the account.
	Personal []impactWorkspace `json:"personal"`
	// Orphaned: shared workspaces where nobody else is an admin — they are
	// handed over, not deleted.
	Orphaned []impactWorkspace `json:"orphaned"`
	// Shared: personal spaces this account invited OTHERS into. They are not
	// destroyed — somebody else's work is in there. They become ordinary
	// workspaces and go to the remaining members; the deleted account's private
	// pages disappear all the same.
	Shared []impactWorkspace `json:"shared"`
	// Pages: pages owned by the account that sit in SHARED workspaces. They
	// stay; the private ones among them become readable to workspace admins
	// only.
	Pages int `json:"pages"`
	// Err: taking stock failed. Then NOTHING may be carried out — an empty plan
	// looks exactly like "nothing hangs off this".
	Err error `json:"-"`
}

type impactWorkspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Pages   int    `json:"pages"`
	Members int    `json:"members"`
	// Heir: who takes the workspace on (empty = the instance owner).
	Heir string `json:"heir,omitempty"`
}

// deletionImpactOf gathers the consequences without changing anything.
func (s *Server) deletionImpactOf(userID string) deletionImpact {
	out := deletionImpact{Personal: []impactWorkspace{}, Orphaned: []impactWorkspace{}, Shared: []impactWorkspace{}}
	if u := s.userByID(userID); u != nil {
		out.UserName = u.Name
	}
	rows, err := s.db.Query(`
		SELECT w.id, w.name, CASE WHEN w.is_personal = 1 AND w.owner_id = ? THEN 1 ELSE 0 END,
		       CASE WHEN w.is_personal = 1 AND w.owner_id != ? THEN 1 ELSE 0 END,
		       (SELECT COUNT(*) FROM pages p WHERE p.workspace_id = w.id AND p.trashed_at IS NULL),
		       (SELECT COUNT(*) FROM workspace_members m2 WHERE m2.workspace_id = w.id)
		FROM workspace_members m JOIN workspaces w ON w.id = m.workspace_id
		WHERE m.user_id = ? ORDER BY w.created_at`, userID, userID, userID)
	if err != nil {
		// Returning quietly would mean: no purge, no handover, and the account
		// deleted anyway — precisely the orphaned state W105 does away with. The
		// caller learns about it through Err.
		log.Printf("deletionImpactOf %s: %v", userID, err)
		out.Err = err
		return out
	}
	type row struct {
		id, name        string
		personal        int
		foreignPersonal int
		pages, members  int
	}
	var all []row
	for rows.Next() {
		var x row
		if rows.Scan(&x.id, &x.name, &x.personal, &x.foreignPersonal, &x.pages, &x.members) == nil {
			all = append(all, x)
		}
	}
	rows.Close() // drain first, then query again (one DB connection)

	for _, x := range all {
		iw := impactWorkspace{ID: x.id, Name: x.name, Pages: x.pages, Members: x.members}
		if x.personal != 0 {
			// Only when truly nobody else is in there. A personal space may have
			// guests (its person invites them) — and then it holds somebody
			// else's work, which deleting an account must not take with it.
			if x.members <= 1 {
				out.Personal = append(out.Personal, iw)
			} else {
				iw.Heir = s.seniorMemberName(x.id, userID)
				out.Shared = append(out.Shared, iw)
			}
			continue
		}
		// SOMEBODY ELSE'S personal space that this account was once invited
		// into: it does not disappear (it belongs to another person) and it is
		// not handed over either. Without this line it fell into the branch for
		// shared workspaces and landed with the instance owner as soon as the
		// invitee was the last remaining member — another human being's personal
		// space, permanently, with no emergency access involved. The membership
		// alone goes by CASCADE on delete.
		if x.foreignPersonal != 0 {
			continue
		}
		// Shared: is there another admin left?
		var heirID, heirName string
		s.db.QueryRow(`SELECT u.id, u.name FROM workspace_members m JOIN users u ON u.id = m.user_id
			WHERE m.workspace_id = ? AND m.role = 'admin' AND m.user_id != ? AND u.disabled = 0
			ORDER BY u.created_at LIMIT 1`, x.id, userID).Scan(&heirID, &heirName)
		if heirID != "" {
			continue // has a successor, no consequence to report
		}
		// The owner only takes over when NOBODY else is a member any more.
		// Otherwise the workspace still belongs to the people in it — and if
		// nobody there is in charge, one of them gets appointed. With the weaker
		// condition, deleting any member at all would have been enough to seat
		// the owner permanently inside somebody else's workspace, as soon as its
		// admins were merely deactivated.
		if x.members > 1 {
			continue
		}
		iw.Heir = heirName // leer -> Instanz-Owner
		out.Orphaned = append(out.Orphaned, iw)
	}
	s.db.QueryRow(`SELECT COUNT(*) FROM pages p JOIN workspaces w ON w.id = p.workspace_id
		WHERE p.owner_id = ? AND p.trashed_at IS NULL AND w.is_personal = 0`, userID).Scan(&out.Pages)
	return out
}

// handleDeletionImpact shows what hangs off an account before it is deleted.
func (s *Server) handleDeletionImpact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if s.userByID(id) == nil {
		httpError(w, 404, "user not found")
		return
	}
	writeJSON(w, s.deletionImpactOf(id))
}

// handleSetUserDisabled deactivates an account or brings it back.
func (s *Server) handleSetUserDisabled(w http.ResponseWriter, r *http.Request) {
	me := requestUser(r)
	id := r.PathValue("id")
	var body struct {
		Disabled bool `json:"disabled"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		httpError(w, 400, "invalid JSON")
		return
	}
	target := s.userByID(id)
	if target == nil {
		httpError(w, 404, "user not found")
		return
	}
	if id == me.ID {
		httpErrorCode(w, 400, "no_self_disable", "You cannot deactivate your own account.")
		return
	}
	// The same guard as for deletion: without an owner the instance is left with
	// nobody in charge, and the role cannot be handed out again from the app.
	if body.Disabled && s.isOwner(id) {
		httpErrorCode(w, 400, "owner_cannot_be_disabled", "The owner cannot be deactivated — hand the owner role on first.")
		return
	}
	v := 0
	action := "enable_user"
	if body.Disabled {
		v = 1
		action = "disable_user"
	}
	if _, err := s.db.Exec(`UPDATE users SET disabled = ? WHERE id = ?`, v, id); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	if body.Disabled {
		// A deactivated account must not keep running merely because it was
		// already signed in. Sessions and tokens end at once.
		s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, id)
		s.db.Exec(`DELETE FROM api_tokens WHERE user_id = ?`, id)
		// The calendar subscription link does not live in api_tokens but as an
		// app_settings row — otherwise it would have gone on serving every date
		// and database in those workspaces indefinitely, with no sign-in.
		s.db.Exec(`DELETE FROM app_settings WHERE key = ?`, "ics_token_"+id)
		// And the open editor: the collab socket is only checked when it
		// connects. Without this, an account deactivated a moment ago carried on
		// writing happily from an open tab — the lock would not have bitten
		// until the next page load.
		s.collab.dropUser(id)
	}
	s.audit("human", me.ID, me.Name, action, "", "", target.Name)
	writeJSON(w, s.userByID(id))
}

// seniorMemberName names the longest-serving active member other than the one given.
func (s *Server) seniorMemberName(wsID, exceptUser string) string {
	var name string
	s.db.QueryRow(`SELECT u.name FROM workspace_members m JOIN users u ON u.id = m.user_id
		WHERE m.workspace_id = ? AND m.user_id != ? AND u.disabled = 0
		ORDER BY CASE m.role WHEN 'admin' THEN 0 WHEN 'member' THEN 1 ELSE 2 END, u.created_at
		LIMIT 1`, wsID, exceptUser).Scan(&name)
	return name
}

// applyDeletion carries out what deletionImpactOf worked out.
//
// Runs AFTER the DELETE on users, with the plan taken beforehand: the account
// is then reliably gone, and if something fails here the worst that remains is
// a workspace with nobody in charge — which the clean-up view lists. The other
// way round (destroy first, then delete) would have been the worst outcome:
// content gone beyond recovery, account still signed in.
func (s *Server) applyDeletion(impact deletionImpact, userID, actorID, actorName string) {
	// Personal spaces go with the person. The content hangs off the workspace
	// row via ON DELETE CASCADE.
	for _, ws := range impact.Personal {
		if err := s.purgeWorkspace(ws.ID); err != nil {
			log.Printf("purge personal workspace %s: %v", ws.ID, err)
			continue
		}
		// Log only AFTER the deed — otherwise the entry claims a deletion that
		// never happened. With no workspace reference, because there is no
		// workspace any more and entries about vanished ones would otherwise
		// expose their page titles.
		s.audit("human", actorID, actorName, "delete_workspace", "", "",
			fmt.Sprintf("%s (personal space of %s, %d pages)", ws.Name, impact.UserName, ws.Pages))
	}

	// Personal spaces WITH guests: somebody else's work is in there. They become
	// ordinary workspaces and stay with the people in them. The deleted
	// account's PRIVATE pages disappear all the same — the guests could never
	// see them, and via the detour "a workspace admin sees everything" they
	// suddenly would.
	for _, ws := range impact.Shared {
		if _, err := s.db.Exec(`DELETE FROM pages_fts WHERE id IN
			(SELECT id FROM pages WHERE workspace_id = ? AND owner_id = ? AND visibility = 'private')`, ws.ID, userID); err != nil {
			log.Printf("shared personal %s: fts: %v", ws.ID, err)
		}
		if _, err := s.db.Exec(`DELETE FROM pages WHERE workspace_id = ? AND owner_id = ? AND visibility = 'private'`, ws.ID, userID); err != nil {
			log.Printf("shared personal %s: private pages: %v", ws.ID, err)
		}
		if _, err := s.db.Exec(`UPDATE workspaces SET is_personal = 0 WHERE id = ?`, ws.ID); err != nil {
			log.Printf("shared personal %s: flag: %v", ws.ID, err)
		}
		// Who takes over: longest-serving active member, failing that the owner.
		var heir string
		s.db.QueryRow(`SELECT m.user_id FROM workspace_members m JOIN users u ON u.id = m.user_id
			WHERE m.workspace_id = ? AND m.user_id != ? AND u.disabled = 0
			ORDER BY CASE m.role WHEN 'admin' THEN 0 WHEN 'member' THEN 1 ELSE 2 END, u.created_at
			LIMIT 1`, ws.ID, userID).Scan(&heir)
		if heir != "" {
			s.db.Exec(`UPDATE workspace_members SET role = 'admin' WHERE workspace_id = ? AND user_id = ?`, ws.ID, heir)
			s.db.Exec(`UPDATE workspaces SET owner_id = ? WHERE id = ?`, heir, ws.ID)
		}
		s.audit("human", actorID, actorName, "workspace_handover", "", ws.ID,
			fmt.Sprintf("%s (was the personal space of %s, has other members)", ws.Name, impact.UserName))
	}

	// Shared ones with no admin left: the instance owner takes over. A workspace
	// with nobody in charge would be invisible to everyone, while its pages went
	// on sitting in the database.
	var ownerID string
	s.db.QueryRow(`SELECT user_id FROM org_members WHERE org_id = ? AND role = ?`, s.defaultOrg(), roleOwner).Scan(&ownerID)
	for _, ws := range impact.Orphaned {
		if ownerID == "" || ownerID == userID {
			continue
		}
		s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')
			ON CONFLICT(workspace_id, user_id) DO UPDATE SET role = 'admin'`, ws.ID, ownerID)
		s.db.Exec(`UPDATE workspaces SET owner_id = ? WHERE id = ?`, ownerID, ws.ID)
		s.audit("human", actorID, actorName, "workspace_handover", "", ws.ID,
			fmt.Sprintf("%s taken over (previously %s)", ws.Name, impact.UserName))
	}

	// All the rest: the deleted account was the owner, but admins remain — the
	// longest-serving inherits, so owner_id does not point at nothing.
	//
	// is_personal = 0: a personal space whose deletion failed above must not
	// wander round the back to the next admin or the instance owner — that is
	// exactly the "pass it quietly to the boss" this module rules out. It stays
	// where it is and shows up in the clean-up view.
	rows, err := s.db.Query(`SELECT id FROM workspaces WHERE owner_id = ? AND is_personal = 0`, userID)
	if err != nil {
		return
	}
	var stillOwned []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			stillOwned = append(stillOwned, id)
		}
	}
	rows.Close()
	for _, wsID := range stillOwned {
		var heir string
		s.db.QueryRow(`SELECT m.user_id FROM workspace_members m JOIN users u ON u.id = m.user_id
			WHERE m.workspace_id = ? AND m.role = 'admin' AND m.user_id != ? AND u.disabled = 0
			ORDER BY u.created_at LIMIT 1`, wsID, userID).Scan(&heir)
		if heir == "" {
			heir = ownerID
		}
		if heir != "" && heir != userID {
			s.db.Exec(`UPDATE workspaces SET owner_id = ? WHERE id = ?`, heir, wsID)
		}
	}
}

// ---- Clean-up: workspaces with nobody in charge --------------------------

type strandedWorkspace struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Owner   string `json:"owner"`
	Members int    `json:"members"`
	Admins  int    `json:"admins"`
	Pages   int    `json:"pages"`
	// Adoptable: truly nobody left. With "members but no admin" the right move
	// is to appoint one of them — not to move in yourself.
	Adoptable bool `json:"adoptable"`
	// Deletable: clearing up works for an orphaned PERSONAL space too — that is
	// exactly the leftover deleting an account produced before W105. Adopting it
	// (and thereby reading it) would be the wrong move there; throwing it away
	// is not.
	Deletable bool `json:"deletable"`
	Personal  bool `json:"personal"`
}

// handleStrandedWorkspaces lists workspaces nobody can look after any more:
// without a member, or without an admin. Leftovers like these could appear
// before W105 (account deleted) and were visible in no interface at all.
func (s *Server) handleStrandedWorkspaces(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT w.id, w.name, COALESCE(u.name, ''), w.is_personal,
		       (SELECT COUNT(*) FROM workspace_members m WHERE m.workspace_id = w.id),
		       (SELECT COUNT(*) FROM workspace_members m JOIN users mu ON mu.id = m.user_id
		         WHERE m.workspace_id = w.id AND m.role = 'admin' AND mu.disabled = 0),
		       (SELECT COUNT(*) FROM pages p WHERE p.workspace_id = w.id AND p.trashed_at IS NULL)
		FROM workspaces w LEFT JOIN users u ON u.id = w.owner_id
		ORDER BY w.created_at`)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	defer rows.Close()
	list := []strandedWorkspace{}
	for rows.Next() {
		var x strandedWorkspace
		var personal int
		if rows.Scan(&x.ID, &x.Name, &x.Owner, &personal, &x.Members, &x.Admins, &x.Pages) != nil {
			continue
		}
		x.Personal = personal != 0
		// A personal space its person is still listed in is not an ownerless
		// leftover — even when the account is deactivated and therefore counts
		// as no ACTIVE admin. Without this line, every deactivation put an entry
		// in the list for which there was neither a button nor any usable
		// advice: the one suggestion ("make one of the members an admin") is
		// barred for personal spaces. Deactivating is the normal case when
		// somebody leaves — the list would have been permanently full of noise,
		// and a genuinely ownerless workspace would have drowned in it.
		if x.Personal && x.Members > 0 {
			continue
		}
		if x.Members > 0 && x.Admins > 0 {
			continue
		}
		x.Adoptable = x.Members == 0 && !x.Personal
		x.Deletable = x.Members == 0
		list = append(list, x)
	}
	writeJSON(w, list)
}

// handleAdoptWorkspace makes the owner an admin of an ownerless workspace.
// Only for ones with nobody in charge — otherwise it would be the self-grant
// W101 expressly did away with (emergency access is what exists for that).
func (s *Server) handleAdoptWorkspace(w http.ResponseWriter, r *http.Request) {
	me := requestUser(r)
	wsID := r.PathValue("id")
	var name string
	if s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, wsID).Scan(&name) != nil {
		httpError(w, 404, "workspace not found")
		return
	}
	// The condition is "no members left", not "no ACTIVE admin".
	//
	// With the weaker condition this would have been a master key: a deactivated
	// account no longer counts as an admin, so "deactivate, then adopt" would
	// have opened any space at all — permanently, where emergency access is
	// time-limited and visible to the people affected. As long as somebody is a
	// member, the workspace belongs to those people; if nobody there is in
	// charge, the owner appoints one from among them (user management) rather
	// than moving in.
	var members, personal int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ?`, wsID).Scan(&members)
	s.db.QueryRow(`SELECT is_personal FROM workspaces WHERE id = ?`, wsID).Scan(&personal)
	if members > 0 {
		httpErrorCode(w, 400, "workspace_has_members", "This workspace still has members. If nobody is in charge, appoint one of them in user management — for a look inside there is emergency access.")
		return
	}
	if personal != 0 {
		httpErrorCode(w, 400, "personal_not_adoptable", "A personal space is not adopted — it belongs to an account.")
		return
	}
	if _, err := s.db.Exec(`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, 'admin')
		ON CONFLICT(workspace_id, user_id) DO UPDATE SET role = 'admin'`, wsID, me.ID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.db.Exec(`UPDATE workspaces SET owner_id = ? WHERE id = ? AND owner_id = ''`, me.ID, wsID)
	s.audit("human", me.ID, me.Name, "workspace_adopted", "", wsID, name)
	writeJSON(w, map[string]any{"ok": true, "name": name})
}

// handleDeleteStrandedWorkspace removes an ownerless workspace and its
// content. Asks for the name as confirmation, as ordinary deletion does.
func (s *Server) handleDeleteStrandedWorkspace(w http.ResponseWriter, r *http.Request) {
	me := requestUser(r)
	wsID := r.PathValue("id")
	var body struct {
		Confirm string `json:"confirm"`
	}
	decodeJSON(w, r, &body)
	var name string
	if s.db.QueryRow(`SELECT name FROM workspaces WHERE id = ?`, wsID).Scan(&name) != nil {
		httpError(w, 404, "workspace not found")
		return
	}
	var members int
	s.db.QueryRow(`SELECT COUNT(*) FROM workspace_members WHERE workspace_id = ?`, wsID).Scan(&members)
	if members > 0 {
		httpErrorCode(w, 400, "workspace_delete_from_inside", "This workspace still has members — it can only be deleted from the inside.")
		return
	}
	if strings.TrimSpace(body.Confirm) != name {
		httpError(w, 400, "confirmation does not match the workspace name")
		return
	}
	if err := s.purgeWorkspace(wsID); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	s.audit("human", me.ID, me.Name, "delete_workspace", "", "", name+" (herrenlos)")
	s.pagesChanged()
	writeJSON(w, map[string]bool{"ok": true})
}
