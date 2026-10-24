package notify

import "testing"

func TestPolicy(t *testing.T) {
	p := Policy{AllowedHosts: []string{"ntfy.sh", "ntfy.example.com"}}
	cases := []struct {
		url   string
		admin bool
		ok    bool
	}{
		// Public services with fixed endpoints are always fine.
		{"discord://token@channel", false, true},
		{"telegram://token@telegram?chats=@chan", false, true},
		{"pushover://shoutrrr:apiToken@userKey", false, true},
		// Self-hosted services only for allowed hosts.
		{"ntfy://ntfy.sh/my-topic", false, true},
		{"ntfy://NTFY.example.com/topic", false, true},
		{"ntfy://paperless:8000/topic", false, false},
		{"generic+http://10.0.0.5:8080/hook", false, false},
		{"generic+https://ntfy.sh/hook", false, true},
		{"smtp://user:pass@mailserver:25/?to=a@b.c", false, false},
		{"gotify://192.168.1.10/token", false, false},
		// Admins may use anything.
		{"generic+http://10.0.0.5:8080/hook", true, true},
		{"smtp://user:pass@mailserver:25/?to=a@b.c", true, true},
	}
	for _, tc := range cases {
		err := p.Check(tc.url, tc.admin)
		if (err == nil) != tc.ok {
			t.Errorf("%s (admin=%v): err=%v, want ok=%v", tc.url, tc.admin, err, tc.ok)
		}
	}
	if err := (Policy{}).Check("ntfy://ntfy.sh/topic", false); err == nil {
		t.Error("with no allowed hosts, self-hosted services must be refused for non-admins")
	}
}
