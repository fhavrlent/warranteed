// Package notify sends warranty expiry reminders via Shoutrrr URLs
// (ntfy, Gotify, Discord, Telegram, email, Pushover, generic webhooks, ...).
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"

	"github.com/fhavrlent/warranteed/internal/dates"
	"github.com/fhavrlent/warranteed/internal/store"
)

type Notifier struct {
	store  *store.Store
	loc    *time.Location
	hour   int
	appURL string
	policy Policy
}

func New(st *store.Store, loc *time.Location, hour int, appURL string, policy Policy) *Notifier {
	return &Notifier{store: st, loc: loc, hour: hour, appURL: strings.TrimSuffix(appURL, "/"), policy: policy}
}

// ParseURLs splits the settings field into individual service URLs.
func ParseURLs(raw string) []string {
	var out []string
	for _, line := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '\r' }) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

// ParseDays parses "30, 7, 1" into a sorted, de-duplicated list.
func ParseDays(raw string) ([]int, error) {
	seen := map[int]bool{}
	var out []int
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 || n > 3650 {
			return nil, fmt.Errorf("%q is not a valid number of days", f)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out, nil
}

// Validate checks that every URL is understood by Shoutrrr.
func Validate(urls []string) error {
	var errs []error
	for _, u := range urls {
		if _, err := shoutrrr.CreateSender(u); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", redact(u), err))
		}
	}
	return errors.Join(errs...)
}

func redact(u string) string {
	if i := strings.Index(u, "://"); i > 0 {
		return u[:i] + "://…"
	}
	return "…"
}

func Send(urls []string, title, message string) error {
	if len(urls) == 0 {
		return errors.New("no notification URLs configured")
	}
	sender, err := shoutrrr.CreateSender(urls...)
	if err != nil {
		return err
	}
	params := types.Params{"title": title}
	var errs []error
	for _, err := range sender.Send(message, &params) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Run checks hourly and sends reminders once the configured hour has passed.
// Each (warranty, threshold, expiry) fires at most once, so restarts or
// multiple checks per day never cause repeats.
func (n *Notifier) Run(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		if time.Now().In(n.loc).Hour() >= n.hour {
			if err := n.CheckAll(ctx); err != nil {
				slog.Error("reminder check failed", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (n *Notifier) CheckAll(ctx context.Context) error {
	ids, err := n.store.ListUserIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := n.checkUser(ctx, id); err != nil {
			slog.Error("reminders for user failed", "user", id, "err", err)
		}
	}
	return nil
}

func (n *Notifier) today() time.Time {
	y, m, d := time.Now().In(n.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (n *Notifier) checkUser(ctx context.Context, userID int64) error {
	st, err := n.store.Settings(ctx, userID)
	if err != nil {
		return err
	}
	admin, err := n.store.IsAdmin(ctx, userID)
	if err != nil {
		return err
	}
	// Re-checked on every send: an admin may have been demoted, or the
	// allowed hosts changed, since the URLs were saved.
	var urls []string
	for _, u := range ParseURLs(st.NotifyURLs) {
		if err := n.policy.Check(u, admin); err != nil {
			slog.Warn("skipping notification URL", "user", userID, "reason", err)
			continue
		}
		urls = append(urls, u)
	}
	thresholds, err := ParseDays(st.NotifyDays)
	if len(urls) == 0 || err != nil || len(thresholds) == 0 {
		return nil
	}
	ws, err := n.store.ActiveWithExpiry(ctx, userID)
	if err != nil {
		return err
	}

	today := n.today()
	type due struct {
		w        *store.Warranty
		daysLeft int
		marks    []int
	}
	var list []due
	for _, w := range ws {
		exp, err := time.Parse(store.DateLayout, *w.ExpiresOn)
		if err != nil {
			continue
		}
		daysLeft := int(exp.Sub(today).Hours() / 24)
		if daysLeft < 0 {
			continue
		}
		// Every threshold we're already inside counts as handled by one
		// message, so adding a warranty 5 days before expiry with thresholds
		// 30,7 sends a single reminder, not two.
		var marks []int
		pending := false
		for _, t := range thresholds {
			if daysLeft <= t {
				marks = append(marks, t)
				sent, err := n.store.ReminderSent(ctx, userID, w.ID, t, *w.ExpiresOn)
				if err != nil {
					return err
				}
				pending = pending || !sent
			}
		}
		if pending {
			list = append(list, due{w, daysLeft, marks})
		}
	}
	if len(list) == 0 {
		return nil
	}

	var b strings.Builder
	for _, d := range list {
		name := d.w.ProductName
		if d.w.Brand != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(d.w.Brand)) {
			name = d.w.Brand + " " + name
		}
		when := fmt.Sprintf("in %d days", d.daysLeft)
		switch d.daysLeft {
		case 0:
			when = "today"
		case 1:
			when = "tomorrow"
		}
		fmt.Fprintf(&b, "• %s — expires %s (%s)\n", name, when, dates.Format(*d.w.ExpiresOn, st.DateFormat))
	}
	if n.appURL != "" {
		fmt.Fprintf(&b, "\n%s", n.appURL)
	}
	title := "Warranty expiring soon"
	if len(list) > 1 {
		title = fmt.Sprintf("%d warranties expiring soon", len(list))
	}
	if err := Send(urls, title, strings.TrimSpace(b.String())); err != nil {
		return err
	}
	for _, d := range list {
		if err := n.store.MarkRemindersSent(ctx, userID, d.w.ID, d.marks, *d.w.ExpiresOn); err != nil {
			return err
		}
	}
	slog.Info("sent expiry reminder", "user", userID, "warranties", len(list))
	return nil
}
