package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kumagaias/tailfeed/internal/api"
	"github.com/kumagaias/tailfeed/internal/db"
)

type feedResponse struct {
	ID      int64  `json:"id"`
	URL     string `json:"url"`
	Title   string `json:"title"`
	GroupID *int64 `json:"groupId"`
}

type addFeedRequest struct {
	URL     string `json:"url"`
	GroupID *int64 `json:"groupId"`
}

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		feeds, err := s.db.ListFeeds(nil)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]feedResponse, 0, len(feeds))
		for _, f := range feeds {
			out = append(out, feedResponse{ID: f.ID, URL: f.URL, Title: f.Title, GroupID: f.GroupID})
		}
		writeJSON(w, out)
	case http.MethodPost:
		var req addFeedRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		req.URL = strings.TrimSpace(req.URL)
		parsed, err := url.ParseRequestURI(req.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			http.Error(w, "feed URL must be an absolute http or https URL", http.StatusBadRequest)
			return
		}
		f, err := s.db.AddFeed(req.URL, req.GroupID)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, db.ErrFeedAlreadyExists) || errors.Is(err, db.ErrMaxFeedsPerGroup) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, feedResponse{ID: f.ID, URL: f.URL, GroupID: f.GroupID})
		if s.pollFeed != nil {
			go s.pollFeed(*f)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/feeds/"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid feed id", http.StatusBadRequest)
		return
	}
	feeds, err := s.db.ListFeeds(nil)
	if err != nil {
		writeError(w, err)
		return
	}
	for _, f := range feeds {
		if f.ID != id {
			continue
		}
		if err := s.db.RemoveFeed(f.URL); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	http.Error(w, "feed not found", http.StatusNotFound)
}

func (s *Server) handleCatalogGenres(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	genres, err := api.CatalogGenres()
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"genres": genres})
}

func (s *Server) handleCatalogFeeds(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	genre := strings.TrimSpace(r.URL.Query().Get("genre"))
	if genre == "" {
		http.Error(w, "genre is required", http.StatusBadRequest)
		return
	}
	feeds, err := api.CatalogFeeds(genre, strings.TrimSpace(r.URL.Query().Get("language")), 100)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]any{"feeds": feeds})
}
