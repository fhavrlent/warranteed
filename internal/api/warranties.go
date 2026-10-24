package api

import (
	"net/http"

	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/store"
)

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, auth.UserFrom(r.Context()))
}

func (s *Server) listWarranties(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListWarranties(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, list)
}

func (s *Server) getWarranty(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	wr, err := s.store.GetWarranty(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, wr)
}

func (s *Server) createWarranty(w http.ResponseWriter, r *http.Request) {
	var in store.WarrantyInput
	if !decode(w, r, &in) {
		return
	}
	id, err := s.store.CreateWarranty(r.Context(), userID(r), &in)
	if err != nil {
		fail(w, err)
		return
	}
	wr, err := s.store.GetWarranty(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, wr)
}

func (s *Server) updateWarranty(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in store.WarrantyInput
	if !decode(w, r, &in) {
		return
	}
	if err := s.store.UpdateWarranty(r.Context(), userID(r), id, &in); err != nil {
		fail(w, err)
		return
	}
	wr, err := s.store.GetWarranty(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, wr)
}

func (s *Server) deleteWarranty(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	paths, err := s.store.DeleteWarranty(r.Context(), userID(r), id)
	if err != nil {
		fail(w, err)
		return
	}
	for _, p := range paths {
		s.files.Remove(p)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) archiveWarranty(archived bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(w, r, "id")
		if !ok {
			return
		}
		if err := s.store.SetArchived(r.Context(), userID(r), id, archived); err != nil {
			fail(w, err)
			return
		}
		wr, err := s.store.GetWarranty(r.Context(), userID(r), id)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, 200, wr)
	}
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context(), userID(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, tags)
}

func (s *Server) updateTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var in struct {
		Name  string `json:"name"`
		Color string `json:"color"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.store.UpdateTag(r.Context(), userID(r), id, in.Name, in.Color); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	if err := s.store.DeleteTag(r.Context(), userID(r), id); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
