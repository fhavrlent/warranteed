package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fhavrlent/warranteed/internal/paperless"
	"github.com/fhavrlent/warranteed/internal/store"
)

// Files stores uploaded files on local disk.
type Files struct{ dir string }

// NewFiles prepares dir and checks it is writable, so a misconfigured mount
// (e.g. an NFS share with the wrong owner) fails at startup instead of on
// the first upload.
func NewFiles(dir string) (*Files, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("files directory %s: %w", dir, err)
	}
	probe, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return nil, fmt.Errorf("files directory %s is not writable (the container runs as uid 65532): %w", dir, err)
	}
	probe.Close()
	os.Remove(probe.Name())
	return &Files{dir: dir}, nil
}

// Save writes r to a new randomly named file and returns its relative name.
func (f *Files) Save(r io.Reader, ext string) (string, int64, error) {
	b := make([]byte, 16)
	rand.Read(b)
	name := hex.EncodeToString(b) + ext
	out, err := os.OpenFile(filepath.Join(f.dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return "", 0, err
	}
	n, err := io.Copy(out, r)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(filepath.Join(f.dir, name))
		return "", 0, err
	}
	return name, n, nil
}

func (f *Files) Open(name string) (*os.File, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(f.dir, name))
}

func (f *Files) Remove(name string) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return
	}
	if err := os.Remove(filepath.Join(f.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("could not remove file", "name", name, "err", err)
	}
}

// inlineTypes are safe to render in the browser; everything else downloads.
var inlineTypes = map[string]bool{
	"application/pdf": true, "image/png": true, "image/jpeg": true, "image/gif": true,
	"image/webp": true, "image/avif": true, "text/plain": true,
}

func cleanFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		name = name[:200-len(ext)] + ext
	}
	return name
}

func detectType(head []byte, filename string) string {
	t := http.DetectContentType(head)
	if t == "application/octet-stream" || strings.HasPrefix(t, "text/plain") {
		if byExt := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); byExt != "" {
			return strings.SplitN(byExt, ";", 2)[0]
		}
	}
	return strings.SplitN(t, ";", 2)[0]
}

func (s *Server) uploadAttachment(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	wid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	wr, err := s.store.GetWarranty(r.Context(), uid, wid)
	if err != nil {
		fail(w, err)
		return
	}

	limit := s.cfg.MaxUploadMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Upload failed — files can be at most %d MB", s.cfg.MaxUploadMB))
		return
	}
	defer r.MultipartForm.RemoveAll()

	kind := r.FormValue("kind")
	if !store.AttachmentKinds[kind] {
		kind = "other"
	}
	target := r.FormValue("storage")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "No file in upload")
		return
	}
	defer file.Close()
	if hdr.Size > limit {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Files can be at most %d MB", s.cfg.MaxUploadMB))
		return
	}

	head := make([]byte, 512)
	n, _ := io.ReadFull(file, head)
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		fail(w, err)
		return
	}
	filename := cleanFilename(hdr.Filename)
	mimeType := detectType(head[:n], filename)
	if kind == "photo" && !strings.HasPrefix(mimeType, "image/") {
		writeError(w, http.StatusBadRequest, "Product photos must be images")
		return
	}

	a := &store.Attachment{
		WarrantyID: wid,
		Kind:       kind,
		Filename:   filename,
		MimeType:   mimeType,
		Size:       hdr.Size,
		Status:     store.StatusReady,
	}

	if target == store.StoragePaperless && kind != "photo" {
		pc, err := s.userPaperless(r.Context(), uid)
		if err != nil {
			fail(w, err)
			return
		}
		if pc == nil {
			writeError(w, http.StatusBadRequest, "Paperless is not configured — set it up in Settings")
			return
		}
		title := strings.TrimSuffix(filename, filepath.Ext(filename))
		if kind == "receipt" || kind == "invoice" {
			title = fmt.Sprintf("%s – %s", wr.ProductName, kind)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		defer cancel()
		taskID, err := pc.Upload(ctx, paperless.Upload{
			Filename: filename,
			Title:    title,
			Created:  wr.PurchaseDate,
			Body:     file,
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		a.Storage, a.PaperlessTaskID, a.Status = store.StoragePaperless, taskID, store.StatusProcessing
		if err := s.store.CreateAttachment(r.Context(), a); err != nil {
			fail(w, err)
			return
		}
		s.wakePoller()
		writeJSON(w, http.StatusCreated, a)
		return
	}

	name, size, err := s.files.Save(file, strings.ToLower(filepath.Ext(filename)))
	if err != nil {
		fail(w, err)
		return
	}
	a.Storage, a.LocalPath, a.Size = store.StorageLocal, name, size
	if err := s.store.CreateAttachment(r.Context(), a); err != nil {
		s.files.Remove(name)
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, a)
}

// linkPaperless attaches documents that already exist in Paperless.
func (s *Server) linkPaperless(w http.ResponseWriter, r *http.Request) {
	uid := userID(r)
	wid, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		DocumentIDs []int64 `json:"documentIds"`
		Kind        string  `json:"kind"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !store.AttachmentKinds[in.Kind] || in.Kind == "photo" {
		in.Kind = "other"
	}
	if len(in.DocumentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "Pick at least one document")
		return
	}
	if _, err := s.store.GetWarranty(r.Context(), uid, wid); err != nil {
		fail(w, err)
		return
	}
	pc, err := s.userPaperless(r.Context(), uid)
	if err != nil {
		fail(w, err)
		return
	}
	if pc == nil {
		writeError(w, http.StatusBadRequest, "Paperless is not configured")
		return
	}
	var out []*store.Attachment
	for _, docID := range in.DocumentIDs {
		doc, err := pc.Document(r.Context(), docID)
		if err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("Document %d: %v", docID, err))
			return
		}
		filename := doc.OriginalFileName
		if doc.Title != "" {
			filename = doc.Title + filepath.Ext(doc.OriginalFileName)
		}
		a := &store.Attachment{
			WarrantyID:     wid,
			Kind:           in.Kind,
			Storage:        store.StoragePaperless,
			Filename:       cleanFilename(filename),
			MimeType:       doc.MimeType,
			PaperlessDocID: &doc.ID,
			Status:         store.StatusReady,
		}
		if err := s.store.CreateAttachment(r.Context(), a); err != nil {
			fail(w, err)
			return
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) patchAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Kind string `json:"kind"`
	}
	if !decode(w, r, &in) {
		return
	}
	a, err := s.store.GetAttachment(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	if !store.AttachmentKinds[in.Kind] || (in.Kind == "photo" && !strings.HasPrefix(a.MimeType, "image/")) {
		writeError(w, http.StatusBadRequest, "Invalid attachment type")
		return
	}
	if err := s.store.UpdateAttachmentKind(r.Context(), id, in.Kind); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteAttachment removes the attachment. Paperless documents are only
// unlinked; they stay in Paperless.
func (s *Server) deleteAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	a, err := s.store.GetAttachment(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.store.DeleteAttachment(r.Context(), id); err != nil {
		fail(w, err)
		return
	}
	if a.Storage == store.StorageLocal {
		s.files.Remove(a.LocalPath)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) retryAttachment(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	a, err := s.store.GetAttachment(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	if a.Storage != store.StoragePaperless || a.PaperlessTaskID == "" {
		writeError(w, http.StatusBadRequest, "Nothing to retry")
		return
	}
	pc, err := s.userPaperless(r.Context(), userID(r))
	if err != nil || pc == nil {
		writeError(w, http.StatusBadRequest, "Paperless is not configured")
		return
	}
	res, err := pc.Task(r.Context(), a.PaperlessTaskID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	switch res.State {
	case paperless.TaskSucceeded:
		err = s.store.CompleteAttachment(r.Context(), id, res.DocID)
	case paperless.TaskFailed:
		err = s.store.FailAttachment(r.Context(), id, res.Error)
	}
	if err != nil {
		fail(w, err)
		return
	}
	a, _ = s.store.GetAttachment(r.Context(), userID(r), id)
	writeJSON(w, 200, a)
}

func (s *Server) attachmentFile(w http.ResponseWriter, r *http.Request) {
	s.serveAttachment(w, r, r.URL.Query().Get("download") == "1", false)
}

func (s *Server) attachmentThumb(w http.ResponseWriter, r *http.Request) {
	s.serveAttachment(w, r, false, true)
}

func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request, download, thumb bool) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	a, err := s.store.GetAttachment(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	// A strict default-src blocks Chrome's built-in PDF viewer, so PDFs only
	// get frame-ancestors; other types never render as active content.
	if a.MimeType == "application/pdf" || (a.Storage == store.StoragePaperless && !thumb) {
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; frame-ancestors 'self'")
	}

	if a.Storage == store.StorageLocal {
		if thumb && !strings.HasPrefix(a.MimeType, "image/") {
			http.NotFound(w, r)
			return
		}
		f, err := s.files.Open(a.LocalPath)
		if err != nil {
			writeError(w, http.StatusNotFound, "File is missing from storage")
			return
		}
		defer f.Close()
		st, _ := f.Stat()
		setDisposition(w, a.Filename, a.MimeType, download)
		w.Header().Set("Content-Type", a.MimeType)
		http.ServeContent(w, r, "", st.ModTime(), f)
		return
	}

	if a.PaperlessDocID == nil {
		writeError(w, http.StatusConflict, "The document is still being processed by Paperless")
		return
	}
	pc, err := s.userPaperless(r.Context(), userID(r))
	if err != nil || pc == nil {
		writeError(w, http.StatusBadGateway, "Paperless is not configured")
		return
	}
	variant := "preview"
	switch {
	case thumb:
		variant = "thumb"
	case download:
		variant = "download"
	}
	s.proxyPaperless(w, r, pc, *a.PaperlessDocID, variant, a.Filename, download)
}

func (s *Server) proxyPaperless(w http.ResponseWriter, r *http.Request, pc *paperless.Client, docID int64, variant, filename string, download bool) {
	resp, err := pc.Stream(r.Context(), docID, variant)
	if err != nil {
		status := http.StatusBadGateway
		if paperless.IsNotFound(err) {
			status = http.StatusNotFound
			err = errors.New("This document no longer exists in Paperless")
		}
		writeError(w, status, err.Error())
		return
	}
	defer resp.Body.Close()
	ct := strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}
	if variant != "thumb" {
		// Paperless serves the archived PDF for previews; keep the name sensible.
		if variant == "preview" && ct == "application/pdf" && !strings.HasSuffix(strings.ToLower(filename), ".pdf") {
			filename = strings.TrimSuffix(filename, filepath.Ext(filename)) + ".pdf"
		}
		setDisposition(w, filename, ct, download)
	}
	io.Copy(w, resp.Body)
}

func setDisposition(w http.ResponseWriter, filename, mimeType string, download bool) {
	disp := "inline"
	if download || !inlineTypes[mimeType] {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": filename}))
}

func (s *Server) searchPaperless(w http.ResponseWriter, r *http.Request) {
	pc, err := s.userPaperless(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	if pc == nil {
		writeError(w, http.StatusBadRequest, "Paperless is not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	docs, err := pc.Search(ctx, r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, 200, docs)
}

func (s *Server) paperlessThumb(w http.ResponseWriter, r *http.Request) {
	docID, err := strconv.ParseInt(r.PathValue("doc"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	pc, err := s.userPaperless(r.Context(), userID(r))
	if err != nil || pc == nil {
		writeError(w, http.StatusBadRequest, "Paperless is not configured")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	s.proxyPaperless(w, r, pc, docID, "thumb", "", false)
}

// ---- background polling of Paperless consumption tasks ----

var pollWake = make(chan struct{}, 1)

func (s *Server) wakePoller() {
	select {
	case pollWake <- struct{}{}:
	default:
	}
}

// PollPaperless resolves pending uploads into document IDs. Consumption
// usually takes seconds (OCR can take minutes), so we poll briefly after an
// upload and back off otherwise. Pending uploads survive restarts.
func (s *Server) PollPaperless(ctx context.Context) {
	const giveUp = 2 * time.Hour
	interval := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-pollWake:
			interval = 2 * time.Second
		case <-time.After(interval):
		}
		pending, err := s.store.PendingPaperless(ctx)
		if err != nil {
			slog.Error("list pending paperless uploads", "err", err)
			continue
		}
		if len(pending) == 0 {
			interval = time.Minute
			continue
		}
		interval = min(interval*2, 30*time.Second)
		for _, p := range pending {
			s.pollOne(ctx, p, giveUp)
		}
	}
}

func (s *Server) pollOne(ctx context.Context, p store.PendingPaperless, giveUp time.Duration) {
	pc, err := s.userPaperless(ctx, p.UserID)
	if err != nil {
		return
	}
	if pc == nil {
		s.store.FailAttachment(ctx, p.AttachmentID, "Paperless is no longer configured")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	res, err := pc.Task(cctx, p.TaskID)
	cancel()
	if err != nil {
		slog.Warn("paperless task lookup failed", "task", p.TaskID, "err", err)
		return
	}
	switch res.State {
	case paperless.TaskSucceeded:
		if err := s.store.CompleteAttachment(ctx, p.AttachmentID, res.DocID); err == nil {
			slog.Info("paperless document ready", "attachment", p.AttachmentID, "doc", res.DocID)
		}
	case paperless.TaskFailed:
		s.store.FailAttachment(ctx, p.AttachmentID, res.Error)
		slog.Warn("paperless consumption failed", "attachment", p.AttachmentID, "err", res.Error)
	default:
		if created, err := time.Parse("2006-01-02T15:04:05Z", p.CreatedAt); err == nil && time.Since(created) > giveUp {
			s.store.FailAttachment(ctx, p.AttachmentID, "Paperless did not finish processing in time — use Retry to check again")
		}
	}
}
