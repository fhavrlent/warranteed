// Package ical writes iCalendar (RFC 5545) feeds of all-day events.
package ical

import (
	"bufio"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Event is one all-day event.
type Event struct {
	UID         string
	Date        time.Time // the day it's on
	Summary     string
	Description string
	URL         string
	Updated     time.Time
}

// Write writes a calendar named name with the events.
func Write(w io.Writer, name string, events []Event) error {
	b := bufio.NewWriter(w)
	line := func(prop, value string) { b.WriteString(fold(prop + ":" + value)) }
	line("BEGIN", "VCALENDAR")
	line("VERSION", "2.0")
	line("PRODID", "-//Warranteed//Warranteed//EN")
	line("CALSCALE", "GREGORIAN")
	line("METHOD", "PUBLISH")
	line("X-WR-CALNAME", escape(name))
	// Ask clients to check for changes a few times a day.
	line("REFRESH-INTERVAL;VALUE=DURATION", "PT6H")
	line("X-PUBLISHED-TTL", "PT6H")
	for _, e := range events {
		line("BEGIN", "VEVENT")
		line("UID", escape(e.UID))
		line("DTSTAMP", e.Updated.UTC().Format("20060102T150405Z"))
		line("DTSTART;VALUE=DATE", e.Date.Format("20060102"))
		line("DTEND;VALUE=DATE", e.Date.AddDate(0, 0, 1).Format("20060102"))
		line("SUMMARY", escape(e.Summary))
		if e.Description != "" {
			line("DESCRIPTION", escape(e.Description))
		}
		if e.URL != "" {
			line("URL", e.URL)
		}
		line("TRANSP", "TRANSPARENT")
		line("END", "VEVENT")
	}
	line("END", "VCALENDAR")
	return b.Flush()
}

// escape escapes a text value.
func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`).Replace(s)
}

// fold ends a content line with CRLF, breaking it into lines of at most 75
// bytes without splitting a character.
func fold(s string) string {
	var b strings.Builder
	limit := 75
	for len(s) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		b.WriteString(s[:cut] + "\r\n ")
		s = s[cut:]
		limit = 74 // the leading space counts
	}
	b.WriteString(s + "\r\n")
	return b.String()
}
