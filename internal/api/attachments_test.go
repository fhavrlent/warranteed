package api

import (
	"strings"
	"testing"

	"github.com/fhavrlent/warranteed/internal/paperless"
)

func TestDetectTypeAndPaperlessSupport(t *testing.T) {
	zipHead := []byte("PK\x03\x04\x14\x00\x06\x00")
	oleHead := []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")
	cases := []struct {
		name      string
		head      []byte
		file      string
		wantType  string
		paperless bool
	}{
		{"pdf", []byte("%PDF-1.4\n"), "receipt.pdf", "application/pdf", true},
		{"jpeg", []byte("\xff\xd8\xff\xe0\x00\x10JFIF"), "photo.jpg", "image/jpeg", true},
		{"text", []byte("hello"), "notes.txt", "text/plain", true},
		{"docx sniffs as zip", zipHead, "terms.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", true},
		{"odt sniffs as zip", zipHead, "terms.odt", "application/vnd.oasis.opendocument.text", true},
		{"legacy doc", oleHead, "terms.doc", "application/msword", true},
		{"zip", zipHead, "firmware.zip", "application/zip", false},
		{"zip named docx-ish", zipHead, "backup.docx.zip", "application/zip", false},
		// The exact type depends on the host's MIME table; only rejection matters.
		{"executable", []byte("MZ\x90\x00"), "setup.exe", "", false},
	}
	for _, tc := range cases {
		got := detectType(tc.head, tc.file)
		if tc.wantType != "" && got != tc.wantType {
			t.Errorf("%s: type %q, want %q", tc.name, got, tc.wantType)
		}
		if paperless.Accepts(got) != tc.paperless {
			t.Errorf("%s: Paperless accepts = %v, want %v", tc.name, !tc.paperless, tc.paperless)
		}
	}
}

func TestPaperlessRejection(t *testing.T) {
	for file, want := range map[string]string{
		"ubuntu.iso":         "Paperless can't store .iso files",
		"backup.tar.gz":      "Paperless can't store .gz files",
		"fake-receipt.pdf":   "“fake-receipt.pdf” isn't really a .pdf file",
		"renamed-terms.DOCX": "“renamed-terms.DOCX” isn't really a .docx file",
		"no-extension":       "Paperless can't store this type of file",
	} {
		if got := paperlessRejection(file).Error(); !strings.HasPrefix(got, want) {
			t.Errorf("%s: %q, want prefix %q", file, got, want)
		}
	}
}
