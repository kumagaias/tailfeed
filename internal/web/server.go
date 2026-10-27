package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kumagaias/tailfeed/internal/db"
)

//go:embed static/*
var embeddedAssets embed.FS

const (
	articlesPageSize = 50
)

// Server exposes a local browser UI for tailfeed.
type Server struct {
	db       *db.DB
	pollFeed func(db.Feed)
}

// New creates a browser UI server.
func New(database *db.DB, pollFeed ...func(db.Feed)) *Server {
	server := &Server{db: database}
	if len(pollFeed) > 0 {
		server.pollFeed = pollFeed[0]
	}
	return server
}

// ListenAndServe starts the browser UI on addr.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, listener)
}

// Serve starts the browser UI on listener.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	handler, err := s.handler()
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		return nil
	}
}

func (s *Server) handler() (http.Handler, error) {
	staticFiles, err := fs.Sub(embeddedAssets, "static")
	if err != nil {
		return nil, fmt.Errorf("load embedded web assets: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(staticFiles))))
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/articles/", s.handleArticleAction)
	mux.HandleFunc("/api/feeds", s.handleFeeds)
	mux.HandleFunc("/api/feeds/", s.handleFeed)
	return logRequest(mux), nil
}

// LocalURL returns the URL clients can open for addr.
func LocalURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, err := embeddedAssets.ReadFile("static/index.html")
	if err != nil {
		slog.Error("failed to read embedded web page", "stage", "read_index", "error", err)
		http.Error(w, "web UI unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(page)
}

type stateResponse struct {
	Groups   []groupResponse   `json:"groups"`
	Selected string            `json:"selected"`
	Query    string            `json:"query"`
	Articles []articleResponse `json:"articles"`
	HasMore  bool              `json:"hasMore"`
}

type groupResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type articleResponse struct {
	ID          int64  `json:"id"`
	FeedTitle   string `json:"feedTitle"`
	Title       string `json:"title"`
	Link        string `json:"link"`
	Summary     string `json:"summary"`
	ImageURL    string `json:"imageUrl"`
	PublishedAt string `json:"publishedAt"`
	Age         string `json:"age"`
	IsRead      bool   `json:"isRead"`
	IsStocked   bool   `json:"isStocked"`
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	selected := r.URL.Query().Get("group")
	if selected == "" {
		selected = "all"
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	offset := 0
	if rawOffset := r.URL.Query().Get("offset"); rawOffset != "" {
		var err error
		offset, err = strconv.Atoi(rawOffset)
		if err != nil || offset < 0 {
			http.Error(w, "invalid offset", http.StatusBadRequest)
			return
		}
	}

	groups, err := s.groups()
	if err != nil {
		writeError(w, err)
		return
	}
	articles, hasMore, err := s.articlePage(selected, query, offset)
	if err != nil {
		writeError(w, err)
		return
	}

	resp := stateResponse{
		Groups:   groups,
		Selected: selected,
		Query:    query,
		Articles: make([]articleResponse, 0, len(articles)),
		HasMore:  hasMore,
	}
	for _, a := range articles {
		resp.Articles = append(resp.Articles, articleJSON(a))
	}
	writeJSON(w, resp)
}

func (s *Server) groups() ([]groupResponse, error) {
	groups, err := s.db.ListGroups()
	if err != nil {
		return nil, err
	}
	out := []groupResponse{
		{ID: "all", Name: "All"},
		{ID: "stock", Name: "Stock"},
	}
	for _, g := range groups {
		out = append(out, groupResponse{ID: strconv.FormatInt(g.ID, 10), Name: g.Name})
	}
	for i := range out {
		articles, err := s.articles(out[i].ID, 0, 0)
		if err != nil {
			return nil, err
		}
		out[i].Count = len(articles)
	}
	return out, nil
}

func (s *Server) articlePage(group, query string, offset int) ([]db.Article, bool, error) {
	if query != "" {
		articles, err := s.articles(group, 0, 0)
		if err != nil {
			return nil, false, err
		}
		articles = filterArticles(articles, query)
		return paginateArticles(articles, offset, articlesPageSize)
	}

	articles, err := s.articles(group, articlesPageSize+1, offset)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(articles) > articlesPageSize
	if hasMore {
		// articles are oldest-first here, so discard the extra oldest item.
		articles = articles[1:]
	}
	return articles, hasMore, nil
}

func paginateArticles(articles []db.Article, offset, limit int) ([]db.Article, bool, error) {
	if offset >= len(articles) {
		return []db.Article{}, false, nil
	}
	end := min(offset+limit, len(articles))
	page := articles[len(articles)-end : len(articles)-offset]
	hasMore := end < len(articles)
	return page, hasMore, nil
}

func (s *Server) articles(group string, limit, offset int) ([]db.Article, error) {
	var (
		articles []db.Article
		err      error
	)
	switch group {
	case "", "all":
		articles, err = s.db.ListArticles(nil, limit, offset)
	case "stock":
		articles, err = s.db.ListStockedArticles(limit, offset)
	default:
		id, parseErr := strconv.ParseInt(group, 10, 64)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid group: %q", group)
		}
		articles, err = s.db.ListArticles(&id, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}
	return articles, nil
}

func filterArticles(articles []db.Article, query string) []db.Article {
	q := strings.ToLower(query)
	out := articles[:0]
	for _, a := range articles {
		if strings.Contains(strings.ToLower(a.Title), q) ||
			strings.Contains(strings.ToLower(stripHTML(a.Summary)), q) ||
			strings.Contains(strings.ToLower(a.FeedTitle), q) {
			out = append(out, a)
		}
	}
	return out
}

func articleJSON(a db.Article) articleResponse {
	published := ""
	age := humanTime(a.CreatedAt)
	if a.PublishedAt != nil {
		published = a.PublishedAt.Format(time.RFC3339)
		age = humanTime(*a.PublishedAt)
	}
	return articleResponse{
		ID:          a.ID,
		FeedTitle:   a.FeedTitle,
		Title:       a.Title,
		Link:        a.Link,
		Summary:     stripHTML(a.Summary),
		ImageURL:    firstImageURL(a.Summary, a.Link),
		PublishedAt: published,
		Age:         age,
		IsRead:      a.IsRead,
		IsStocked:   a.IsStocked,
	}
}

func (s *Server) handleArticleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/articles/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid article id", http.StatusBadRequest)
		return
	}
	switch parts[1] {
	case "read":
		err = s.db.MarkRead(id)
	case "stock":
		err = s.db.ToggleStock(id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("web request", "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range html.UnescapeString(s) {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

var imgSrcRe = regexp.MustCompile(`(?is)<img[^>]+src\s*=\s*["']?([^"' >]+)`)

func firstImageURL(summary, articleLink string) string {
	match := imgSrcRe.FindStringSubmatch(summary)
	if len(match) < 2 {
		return ""
	}
	raw := html.UnescapeString(strings.TrimSpace(match[1]))
	if raw == "" || strings.HasPrefix(strings.ToLower(raw), "data:") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	base, err := url.Parse(articleLink)
	if err != nil {
		return ""
	}
	return base.ResolveReference(u).String()
}

func humanTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
