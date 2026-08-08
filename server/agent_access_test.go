package server

import (
	"strings"
	"testing"
)

// A workspace may now say what an AGENT is allowed to do in it — the thing it
// could not say before, when the reach of every agent was decided entirely by
// whoever issued the credential.
//
// Opt-in: the default is what exists today. An instance that updates and
// changes nothing must notice nothing, which is the first case below.
func TestAWorkspaceCanSayWhatAgentsMayDoInIt(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "modes@example.test")
	ws := s.firstWorkspaceOf(t, uid)

	session := &user{ID: uid, Name: "Jeremia"}
	apiToken := &user{ID: uid, Name: "Jeremia", TokenScope: "write", TokenKind: tokenKindAPI}
	signedIn := &user{ID: uid, Name: "Jeremia", TokenScope: "write", TokenKind: tokenKindOAuth}

	set := func(mode string) {
		t.Helper()
		if _, err := s.db.Exec(`UPDATE workspaces SET agent_access = ? WHERE id = ?`, mode, ws); err != nil {
			t.Fatalf("set %s: %v", mode, err)
		}
	}

	// Untouched: everything as before.
	for _, u := range []*user{session, apiToken, signedIn} {
		if !s.credentialMayEnter(u, ws) {
			t.Error("an untouched workspace turned somebody away — the default is not the old behaviour")
		}
	}
	set("")
	if !s.credentialMayEnter(apiToken, ws) {
		t.Error("an empty setting is not read as open")
	}

	// strict: the point of the whole exercise. A permanent token is refused even
	// though it names this workspace; a signed-in connection is not.
	set(agentAccessStrict)
	if s.credentialMayEnter(apiToken, ws) {
		t.Error("a permanent API token got into a strict workspace")
	}
	if !s.credentialMayEnter(signedIn, ws) {
		t.Error("a signed-in connection was refused by a strict workspace — that is the one it is for")
	}
	if !s.credentialMayEnter(session, ws) {
		t.Error("a person in a browser was turned away by a rule about agents")
	}

	// closed: no agent at all, however it arrived.
	set(agentAccessClosed)
	for _, u := range []*user{apiToken, signedIn} {
		if s.credentialMayEnter(u, ws) {
			t.Error("an agent got into a closed workspace")
		}
	}
	if !s.credentialMayEnter(session, ws) {
		t.Error("a closed workspace locked out its own people")
	}

	// Nonsense in the column reads as open rather than as a lockout: a typo in a
	// setting must not take a workspace offline.
	set("bananas")
	if !s.credentialMayEnter(apiToken, ws) {
		t.Error("an unrecognised value locked the workspace instead of falling back to open")
	}
}

// The rule has to bite where it counts — in the enumerations an agent uses to
// find its way around, not only in a helper nobody calls.
func TestAStrictWorkspaceDisappearsFromAnAPITokensView(t *testing.T) {
	s := testServer(t)
	uid, _ := signedIn(t, s, "strict-list@example.test")
	open := s.firstWorkspaceOf(t, uid)
	strict := makeNamedWorkspace(t, s, uid, "Dokumentation")
	if _, err := s.db.Exec(`UPDATE workspaces SET agent_access = ? WHERE id = ?`, agentAccessStrict, strict); err != nil {
		t.Fatalf("set: %v", err)
	}

	apiToken := &user{ID: uid, Name: "Jeremia", TokenScope: "write", TokenKind: tokenKindAPI}
	listed, err := s.mcpListWorkspaces(apiToken)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(listed, "Dokumentation") || strings.Contains(listed, strict) {
		t.Errorf("a strict workspace was listed to a permanent token:\n%s", listed)
	}
	if !strings.Contains(listed, open) {
		t.Error("the open workspace vanished too")
	}

	// And it IS there for a connection somebody signed in for.
	grant := &user{ID: uid, Name: "Jeremia", TokenScope: "write", TokenKind: tokenKindOAuth}
	listed, err = s.mcpListWorkspaces(grant)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(listed, strict) {
		t.Error("a signed-in connection cannot see the workspace that was made strict for it")
	}
}
