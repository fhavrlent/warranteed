package ical

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWrite(t *testing.T) {
	var b bytes.Buffer
	day := time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)
	err := Write(&b, "Warranties", []Event{{
		UID: "warranty-1@example.com", Date: day, Summary: "Ends: Kettle, steel; 1.7 l",
		Description: "Line one\nLine two", URL: "https://example.com/w/1", Updated: day,
	}})
	if err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\n", "DTSTART;VALUE=DATE:20270331\r\n", "DTEND;VALUE=DATE:20270401\r\n",
		`SUMMARY:Ends: Kettle\, steel\; 1.7 l` + "\r\n", `DESCRIPTION:Line one\nLine two` + "\r\n", "END:VCALENDAR\r\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
		t.Error("bare newline in output")
	}
}

func TestFold(t *testing.T) {
	long := "SUMMARY:" + strings.Repeat("ž", 60) // 2 bytes each
	folded := fold(long)
	for _, l := range strings.Split(strings.TrimSuffix(folded, "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Errorf("line of %d bytes", len(l))
		}
		if !strings.HasPrefix(l, "SUMMARY") && !strings.HasPrefix(l, " ") {
			t.Errorf("continuation without a space: %q", l)
		}
	}
	unfolded := strings.ReplaceAll(strings.TrimSuffix(folded, "\r\n"), "\r\n ", "")
	if unfolded != long {
		t.Error("folding changed the text")
	}
}
