package notify

import (
	"net/url"
	"slices"
	"strings"

	"github.com/fhavrlent/warranteed/internal/i18n"
)

// publicServices always send to their own fixed, public endpoints (e.g.
// discord.com, api.telegram.org); the "host" part of their URLs is a token or
// channel, not an address. Verified against the services' source.
var publicServices = map[string]bool{
	"discord":    true,
	"telegram":   true,
	"pushover":   true,
	"pushbullet": true,
	"slack":      true,
	"ifttt":      true,
	"join":       true,
}

// Policy decides which notification URLs a user may use. Admins may use any.
// Everyone else may use public services, and services where the host is part
// of the URL (ntfy, Gotify, generic webhooks, SMTP, ...) only for hosts in
// AllowedHosts, so the server can't be used to reach internal addresses.
type Policy struct {
	AllowedHosts []string
}

func (p Policy) Check(rawURL string, admin bool) error {
	if admin {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return i18n.NewError("server.notifyInvalidUrl", nil)
	}
	scheme := strings.ToLower(u.Scheme)
	if publicServices[scheme] {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if host != "" && slices.Contains(p.AllowedHosts, host) {
		return nil
	}
	if len(p.AllowedHosts) == 0 {
		return i18n.NewError("server.notifyNeedsAdmin", i18n.Args{"scheme": scheme})
	}
	return i18n.NewError("server.notifyHostNotAllowed", i18n.Args{"host": host, "allowed": strings.Join(p.AllowedHosts, ", ")})
}

func (p Policy) CheckAll(urls []string, admin bool) error {
	for _, u := range urls {
		if err := p.Check(u, admin); err != nil {
			return err
		}
	}
	return nil
}
