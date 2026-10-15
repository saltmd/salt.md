package server

import (
	"database/sql"
	"encoding/json"
)

type pageMention struct {
	ID           string
	UserID       string
	AuthorID     string
	OriginPageID string
	Excerpt      string
}

// Walk every inline container, including nested blocks, links and table cells.
// Notifications retain an immutable occurrence ID even after undo/redo or moves.
func extractPageMentions(raw []byte) []pageMention {
	var root any
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return nil
	}
	result := []pageMention{}
	var walk func(any, string)
	walk = func(value any, excerpt string) {
		switch node := value.(type) {
		case map[string]any:
			if _, block := node["id"].(string); block {
				content, _ := json.Marshal(node["content"])
				excerpt = extractText(content)
			}
			if node["type"] == "userMention" {
				props, _ := node["props"].(map[string]any)
				id, _ := props["mentionId"].(string)
				userID, _ := props["userId"].(string)
				authorID, _ := props["authorId"].(string)
				origin, _ := props["originPageId"].(string)
				if id != "" && userID != "" && len(id) <= 100 && len(userID) <= 100 {
					result = append(result, pageMention{id, userID, authorID, origin, excerpt})
				}
				return
			}
			for _, child := range node {
				walk(child, excerpt)
			}
		case []any:
			for _, child := range node {
				walk(child, excerpt)
			}
		}
	}
	walk(root, "")
	return result
}

func (s *Server) eligiblePageMentions(pageID string, content []byte, author *user) []pageMention {
	result := []pageMention{}
	eligible := map[string]bool{}
	checked := map[string]bool{}
	for _, m := range extractPageMentions(content) {
		// Copying a page/template or another person's tag is not a fresh invitation.
		// A recipient's later rename does not change the selected account.
		if m.OriginPageID != pageID || m.AuthorID != author.ID || m.UserID == author.ID {
			continue
		}
		if !checked[m.UserID] {
			eligible[m.UserID] = s.mentionable(m.UserID, pageID)
			checked[m.UserID] = true
		}
		if eligible[m.UserID] {
			result = append(result, m)
		}
	}
	return result
}

func insertPageMentions(tx *sql.Tx, pageID string, mentions []pageMention, author *user) error {
	for _, m := range mentions {
		_, err := tx.Exec(`INSERT INTO page_mention_notifications(id,page_id,mention_id,user_id,author_id,author_name,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(page_id,mention_id,user_id) DO NOTHING`, newID(), pageID, m.ID, m.UserID, author.ID, author.Name, now())
		if err != nil {
			return err
		}
	}
	return nil
}
