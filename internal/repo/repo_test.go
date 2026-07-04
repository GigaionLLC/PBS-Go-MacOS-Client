package repo

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in    string
		user  string
		host  string
		port  int
		store string
		err   bool
	}{
		{in: "store", user: DefaultUser, port: DefaultPort, store: "store"},
		{in: "pbs.example.com:store", user: DefaultUser, host: "pbs.example.com", port: DefaultPort, store: "store"},
		{in: "pbs.example.com:1234:store", user: DefaultUser, host: "pbs.example.com", port: 1234, store: "store"},
		{in: "backup@pbs@pbs.example.com:store", user: "backup@pbs", host: "pbs.example.com", port: DefaultPort, store: "store"},
		{in: "root@pam@10.0.0.5:8007:vault", user: "root@pam", host: "10.0.0.5", port: 8007, store: "vault"},
		{in: "", err: true},
		{in: "a:b:c:d", err: true},
		{in: "host:notaport:store", err: true},
	}
	for _, c := range cases {
		got, err := Parse(c.in)
		if c.err {
			if err == nil {
				t.Errorf("Parse(%q): expected error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got.User != c.user || got.Host != c.host || got.Port != c.port || got.Datastore != c.store {
			t.Errorf("Parse(%q) = %+v, want user=%q host=%q port=%d store=%q",
				c.in, got, c.user, c.host, c.port, c.store)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	specs := []string{"store", "pbs.example.com:store", "backup@pbs@pbs.example.com:store"}
	for _, s := range specs {
		r, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q): %v", s, err)
		}
		if got := r.String(); got != s {
			t.Errorf("round-trip %q -> %q", s, got)
		}
	}
}
