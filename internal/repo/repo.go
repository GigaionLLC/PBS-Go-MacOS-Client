// Package repo parses PBS repository specifications of the form
//
//	[[username@realm]@host[:port]:]datastore
//
// matching the syntax understood by the official proxmox-backup-client and the
// PBS_REPOSITORY environment variable.
package repo

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// DefaultPort is the PBS API/HTTPS port.
const DefaultPort = 8007

// DefaultUser is assumed when a repository omits the user@realm part.
const DefaultUser = "root@pam"

// Repository is a parsed PBS repository target.
type Repository struct {
	User      string // e.g. "root@pam" or "backup@pbs"
	Host      string // hostname or IP; empty means local/unset
	Port      int    // TLS port, defaults to DefaultPort
	Datastore string // datastore name (required)
}

// Parse decodes a repository spec. The datastore is mandatory; user, host and
// port are optional and take documented defaults when omitted.
func Parse(s string) (*Repository, error) {
	if strings.TrimSpace(s) == "" {
		return nil, errors.New("empty repository spec")
	}

	r := &Repository{User: DefaultUser, Port: DefaultPort}

	// Split off "user@" if present. The user itself contains an '@' (user@realm),
	// so we split on the LAST '@' that precedes the host:datastore part. We do
	// this by locating the separator '@' as the one whose remainder still holds
	// a ':' (host:datastore) — but the common, unambiguous forms are handled by
	// splitting on '@' only when a host/datastore ':' follows.
	rest := s
	if at := strings.LastIndex(s, "@"); at != -1 {
		// Everything after the last '@' is host[:port]:datastore or datastore.
		// Everything before is user@realm.
		candidateUser := s[:at]
		candidateRest := s[at+1:]
		if candidateUser != "" && strings.Contains(candidateUser, "@") {
			r.User = candidateUser
			rest = candidateRest
		}
	}

	// rest is now "host[:port]:datastore" or "datastore".
	parts := strings.Split(rest, ":")
	switch len(parts) {
	case 1:
		r.Datastore = parts[0]
	case 2:
		// host:datastore
		r.Host = parts[0]
		r.Datastore = parts[1]
	case 3:
		// host:port:datastore
		r.Host = parts[0]
		p, err := strconv.Atoi(parts[1])
		if err != nil || p <= 0 || p > 65535 {
			return nil, fmt.Errorf("invalid port %q", parts[1])
		}
		r.Port = p
		r.Datastore = parts[2]
	default:
		return nil, fmt.Errorf("malformed repository spec %q", s)
	}

	if r.Datastore == "" {
		return nil, errors.New("repository spec is missing a datastore")
	}
	return r, nil
}

// BaseURL returns the https base URL for the repository's host, or an error if
// the repository has no host (a local/unset target cannot be reached).
func (r *Repository) BaseURL() (string, error) {
	if r.Host == "" {
		return "", errors.New("repository has no host")
	}
	return fmt.Sprintf("https://%s:%d", r.Host, r.Port), nil
}

// String renders the repository back to its canonical spec form.
func (r *Repository) String() string {
	var b strings.Builder
	if r.User != "" && r.User != DefaultUser {
		b.WriteString(r.User)
		b.WriteByte('@')
	}
	if r.Host != "" {
		b.WriteString(r.Host)
		if r.Port != DefaultPort {
			fmt.Fprintf(&b, ":%d", r.Port)
		}
		b.WriteByte(':')
	}
	b.WriteString(r.Datastore)
	return b.String()
}
