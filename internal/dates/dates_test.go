package dates

import "testing"

func TestFormatMonthNames(t *testing.T) {
	for _, tc := range []struct{ format, lang, want string }{
		{"d MMMM yyyy", "en", "31 December 2025"},
		{"d MMMM yyyy", "cs", "31 prosince 2025"},
		{"MMM d, yyyy", "cs", "pro 31, 2025"},
		{"dd.MM.yyyy", "cs", "31.12.2025"},
		{"d MMMM yyyy", "xx", "31 December 2025"},
	} {
		if got := Format("2025-12-31", tc.format, tc.lang); got != tc.want {
			t.Errorf("Format(%q, %q) = %q, want %q", tc.format, tc.lang, got, tc.want)
		}
	}
}
