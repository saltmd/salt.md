package server

import "net/http"

// Favorites are per-user pinned pages, shown at the top of the sidebar.

func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT f.page_id FROM favorites f
		JOIN pages p ON p.id = f.page_id
		WHERE f.user_id = ? AND p.trashed_at IS NULL
		ORDER BY f.position, f.page_id`, requestUser(r).ID)
	if err != nil {
		httpError(w, 500, err.Error())
		return
	}
	userID := requestUser(r).ID
	var cand []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			httpError(w, 500, err.Error())
			return
		}
		cand = append(cand, id)
	}
	rows.Close()
	// Filter after draining (single DB connection — see handleSearch).
	ids := []string{}
	for _, id := range cand {
		if s.canRead(userID, id) {
			ids = append(ids, id)
		}
	}
	writeJSON(w, ids)
}

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	pageID := r.PathValue("id")
	userID := requestUser(r).ID
	if !s.canRead(userID, pageID) {
		httpError(w, 404, "page not found")
		return
	}
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM pages WHERE id = ? AND trashed_at IS NULL`, pageID).Scan(&exists); err != nil || exists == 0 {
		httpError(w, 404, "page not found")
		return
	}
	var pos float64
	s.db.QueryRow(`SELECT COALESCE(MAX(position), 0) + 1 FROM favorites WHERE user_id = ?`, userID).Scan(&pos)
	if _, err := s.db.Exec(`INSERT INTO favorites (user_id, page_id, position) VALUES (?, ?, ?)
		ON CONFLICT(user_id, page_id) DO NOTHING`, userID, pageID, pos); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	if _, err := s.db.Exec(`DELETE FROM favorites WHERE user_id = ? AND page_id = ?`, requestUser(r).ID, r.PathValue("id")); err != nil {
		httpError(w, 500, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
