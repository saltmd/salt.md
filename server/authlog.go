package server

import (
	"log"
	"net/http"
)

// A log line for every rejected credential — the one thing fail2ban needs and
// the one thing this server never wrote. Before this it logged nothing at all
// beyond its three startup lines, so a machine on the open internet could be
// knocked on all night without leaving a trace anywhere a jail could read.
//
// WHAT IS IN THE LINE, and what deliberately is not:
//
//   - the address, because that is what gets banned;
//   - the kind of credential, so a jail can weigh a wrong password differently
//     from a wrong token;
//   - NOT the email, and NOT the token. A ban log is read by a daemon and ends
//     up in journald, in log shipping and in backups; the audit table is where
//     "who did what" belongs, behind a login. Writing an address book into
//     syslog to save one lookup is a bad trade.
//
// The format is fixed on purpose — it is a parsing contract, not prose. The
// filter that goes with it lives in docs/fail2ban/.
func (s *Server) logAuthFailure(r *http.Request, kind string) {
	log.Printf("auth: rejected %s from %s", kind, s.clientIP(r))
}
