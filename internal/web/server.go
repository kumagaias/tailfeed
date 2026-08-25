package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
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

const (
	articlesPageSize = 50
	articlesLimit    = 1000
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
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/articles/", s.handleArticleAction)
	mux.HandleFunc("/api/feeds", s.handleFeeds)
	mux.HandleFunc("/api/feeds/", s.handleFeed)
	mux.HandleFunc("/api/catalog/genres", s.handleCatalogGenres)
	mux.HandleFunc("/api/catalog/feeds", s.handleCatalogFeeds)

	server := &http.Server{
		Handler:           logRequest(mux),
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
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
		articles, err := s.articles(out[i].ID, articlesLimit, 0)
		if err != nil {
			return nil, err
		}
		out[i].Count = len(articles)
	}
	return out, nil
}

func (s *Server) articlePage(group, query string, offset int) ([]db.Article, bool, error) {
	if query != "" {
		articles, err := s.articles(group, articlesLimit, 0)
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

const indexHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tailfeed</title>
<style>
:root {
  color-scheme: light dark;
  --bg: #f6f7f9;
  --panel: #ffffff;
  --detail-bg: #fbfcfd;
  --input-bg: #fbfcfd;
  --text: #15171a;
  --title: #111418;
  --summary: #424951;
  --muted: #69707a;
  --line: #d9dee5;
  --accent: #0f766e;
  --accent-strong: #0b5f59;
  --read: #7c8591;
  --stock: #c0264e;
  --sidebar-bg: #111418;
  --sidebar-line: #0b0d10;
  --sidebar-text: #f7f8fa;
  --sidebar-muted: #c6ccd4;
  --sidebar-hover: #1c2229;
  --thumb-bg: #eef1f4;
  --thumb-placeholder: #e7ebf0;
  --shadow: 0 1px 2px rgba(18, 24, 31, .08);
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #111418;
    --panel: #191d22;
    --detail-bg: #15191e;
    --input-bg: #111418;
    --text: #edf0f3;
    --title: #f7f8fa;
    --summary: #d7dce2;
    --muted: #a2abb6;
    --line: #303842;
    --accent: #2f8f85;
    --accent-strong: #48aaa0;
    --read: #89939f;
    --stock: #f06282;
    --sidebar-bg: #0d1014;
    --sidebar-line: #242a31;
    --sidebar-text: #f7f8fa;
    --sidebar-muted: #b4bdc8;
    --sidebar-hover: #20262d;
    --thumb-bg: #242a31;
    --thumb-placeholder: #20262d;
    --shadow: 0 1px 2px rgba(0, 0, 0, .35);
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  height: 100vh;
  overflow: hidden;
  background: var(--bg);
  color: var(--text);
  font: 14px/1.45 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
button, input { font: inherit; }
.app {
  display: grid;
  grid-template-columns: 240px minmax(0, 1fr);
  height: 100vh;
  overflow: hidden;
}
.sidebar {
  height: 100vh;
  overflow-y: auto;
  background: var(--sidebar-bg);
  color: var(--sidebar-text);
  padding: 18px 12px;
  border-right: 1px solid var(--sidebar-line);
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 8px 18px;
  font-size: 18px;
  font-weight: 700;
}
.brand-mark {
  display: inline-grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 6px;
  background: var(--accent);
  color: white;
  font-weight: 800;
}
.groups {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.feed-manager {
  margin-top: 18px;
  padding: 14px 8px 0;
  border-top: 1px solid var(--sidebar-line);
}
.feed-manager summary { color: var(--sidebar-muted); cursor: pointer; font-weight: 700; }
.feed-form { display: flex; gap: 6px; margin-top: 10px; }
.feed-form input {
  min-width: 0;
  width: 100%;
  padding: 7px 8px;
  border: 1px solid #343b44;
  border-radius: 6px;
  background: #171b20;
  color: var(--sidebar-text);
}
.feed-form button, .feed-remove {
  border: 1px solid #343b44;
  border-radius: 6px;
  background: var(--sidebar-hover);
  color: var(--sidebar-text);
  cursor: pointer;
}
.feed-form button { padding: 6px 9px; }
.feed-list { margin-top: 10px; display: grid; gap: 6px; }
.feed-row { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 6px; align-items: center; }
.feed-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--sidebar-muted); font-size: 12px; }
.feed-remove { padding: 2px 6px; color: #f7a7b9; }
.catalog-controls { display: grid; grid-template-columns: 1fr 72px; gap: 6px; margin-top: 12px; }
.catalog-controls select {
  min-width: 0;
  padding: 6px;
  border: 1px solid #343b44;
  border-radius: 6px;
  background: #171b20;
  color: var(--sidebar-text);
}
.catalog-results { display: grid; gap: 7px; margin-top: 10px; max-height: 260px; overflow: auto; }
.catalog-feed { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 7px; align-items: start; color: var(--sidebar-muted); font-size: 12px; }
.catalog-feed input { margin-top: 3px; }
.catalog-feed small { display: block; color: #8893a0; }
.catalog-add { width: 100%; margin-top: 10px; padding: 7px; }
.group {
  width: 100%;
  border: 0;
  background: transparent;
  color: var(--sidebar-muted);
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
  text-align: left;
  padding: 9px 10px;
  border-radius: 6px;
  cursor: pointer;
}
.group:hover { background: var(--sidebar-hover); color: white; }
.group.active { background: var(--accent); color: white; }
.group-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.group-count {
  min-width: 2ch;
  color: inherit;
  opacity: .75;
  font-size: 12px;
  text-align: right;
}
.main {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  min-width: 0;
  min-height: 0;
  height: 100vh;
  overflow: hidden;
}
.toolbar {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 280px auto;
  gap: 18px;
  align-items: center;
  min-height: 68px;
  padding: 14px 22px;
  background: var(--panel);
  border-bottom: 1px solid var(--line);
}
.title {
  display: flex;
  align-items: baseline;
  gap: 12px;
  min-width: 0;
}
.title h1 {
  margin: 0;
  font-size: 18px;
  line-height: 1.2;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.meta { color: var(--muted); font-size: 13px; }
.search {
  width: 100%;
  border: 1px solid var(--line);
  border-radius: 6px;
  padding: 9px 11px;
  background: var(--input-bg);
  color: var(--text);
}
.content {
  display: grid;
  grid-template-columns: minmax(420px, 63%) minmax(320px, 37%);
  min-height: 0;
  overflow: hidden;
}
.content.detail-closed { grid-template-columns: minmax(0, 1fr); }
.content.detail-closed .list { border-right: 0; }
.content.detail-closed .detail { display: none; }
.list {
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;
  -webkit-overflow-scrolling: touch;
  border-right: 1px solid var(--line);
  padding: 14px;
}
.article {
  width: 100%;
  text-align: left;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--panel);
  box-shadow: var(--shadow);
  padding: 13px 14px;
  margin: 0 0 10px;
  cursor: pointer;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 72px;
  gap: 10px;
}
.article:hover { border-color: #b7c0ca; }
.article.active { border-color: var(--accent); box-shadow: 0 0 0 2px rgba(15, 118, 110, .14); }
.article.read .article-title { color: var(--read); font-weight: 700; }
.article-media {
  display: grid;
  place-items: center;
  width: 72px;
  height: 54px;
  align-self: start;
  border-radius: 6px;
  border: 1px solid var(--line);
  background: var(--thumb-placeholder);
  overflow: hidden;
}
.article-thumb,
.article-placeholder {
  width: 100%;
  height: 100%;
}
.article-thumb {
  object-fit: cover;
  background: var(--thumb-bg);
}
.article-placeholder {
  display: grid;
  place-items: center;
  color: var(--muted);
  font-size: 15px;
  font-weight: 800;
  text-transform: uppercase;
}
.article-top {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.stock {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  color: #aab2bd;
  flex: 0 0 auto;
  border-radius: 999px;
}
.stock:hover {
  background: color-mix(in srgb, var(--stock) 12%, transparent);
  color: var(--stock);
}
.stock.on { color: var(--stock); }
.article-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 15px;
  line-height: 1.35;
  font-weight: 800;
  color: var(--title);
}
.article-meta {
  margin-top: 5px;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 12px;
}
.article-summary {
  margin-top: 8px;
  color: var(--summary);
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.detail {
  position: relative;
  min-height: 0;
  overflow: auto;
  padding: 28px 34px;
  background: var(--detail-bg);
}
.detail-close {
  position: sticky;
  top: 0;
  z-index: 1;
  float: right;
  margin: -16px -22px 8px 16px;
}
.detail h2 {
  margin: 0 0 10px;
  font-size: 25px;
  line-height: 1.25;
  letter-spacing: 0;
}
.detail-meta { color: var(--muted); margin-bottom: 22px; }
.detail-image {
  display: none;
  width: min(100%, 380px);
  max-height: 190px;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid var(--line);
  margin-bottom: 22px;
  background: var(--thumb-bg);
}
.detail-summary {
  white-space: pre-wrap;
  color: var(--summary);
  font-size: 15px;
  max-width: 76ch;
}
.actions {
  display: flex;
  gap: 8px;
  margin-top: 24px;
}
.action {
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--panel);
  color: var(--text);
  padding: 8px 11px;
  cursor: pointer;
}
.action.primary {
  background: var(--accent);
  border-color: var(--accent-strong);
  color: white;
}
.empty {
  color: var(--muted);
  padding: 24px;
}
.loading {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--muted);
  padding: 24px;
  text-align: center;
  font-size: 12px;
}
.loading.compact { padding: 4px 0 14px; }
.load-more {
  border: 1px solid var(--line);
  border-radius: 6px;
  background: var(--panel);
  color: var(--text);
  padding: 6px 10px;
  cursor: pointer;
}
.load-more:hover { border-color: var(--accent); color: var(--accent-strong); }
.loading-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid var(--line);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: spin .7s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 860px) {
  body { height: auto; overflow: auto; }
  .app { grid-template-columns: 1fr; height: auto; overflow: visible; }
  .sidebar { height: auto; overflow: visible; }
  .groups { flex-direction: row; overflow-x: auto; }
  .group { min-width: 140px; }
  .main { height: auto; overflow: visible; }
  .toolbar { grid-template-columns: minmax(0, 1fr) auto; gap: 10px; }
  .search { grid-column: 1 / -1; }
  .content { grid-template-columns: 1fr; }
  .list { height: 60vh; max-height: 60vh; border-right: 0; }
  .detail { border-top: 1px solid var(--line); }
  .article { grid-template-columns: minmax(0, 1fr) 58px; }
  .article-media { width: 58px; height: 44px; }
}
</style>
</head>
<body>
<div class="app">
  <aside class="sidebar">
    <div class="brand"><span class="brand-mark">tf</span><span>tailfeed</span></div>
    <nav id="groups" class="groups"></nav>
    <details class="feed-manager">
      <summary>Manage feeds</summary>
      <form id="feedForm" class="feed-form">
        <input id="feedURL" type="url" required placeholder="https://…" aria-label="Feed URL">
        <button type="submit">Add</button>
      </form>
      <div class="catalog-controls">
        <select id="catalogGenre" aria-label="Feed genre"><option value="">Browse catalog…</option></select>
        <select id="catalogLanguage" aria-label="Feed language"><option value="">All</option><option value="ja">日本語</option><option value="en">English</option></select>
      </div>
      <div id="catalogResults" class="catalog-results"></div>
      <button id="catalogAdd" class="feed-form catalog-add" type="button" hidden>Add selected</button>
      <div id="feedList" class="feed-list"></div>
    </details>
  </aside>
  <main class="main">
    <header class="toolbar">
      <div class="title">
        <h1 id="currentGroup">All</h1>
        <span id="articleCount" class="meta">0 articles</span>
      </div>
      <input id="search" class="search" type="search" placeholder="Search articles">
      <button id="detailToggle" class="action" type="button">Hide detail</button>
    </header>
    <section id="content" class="content">
      <div id="list" class="list"></div>
      <article id="detail" class="detail"></article>
    </section>
  </main>
</div>
<script>
const pageSize = 50;
const state = {
  group: "all", query: "", articles: [], selected: null,
  groups: [], hasMore: false, loading: false, pendingG: false, detailOpen: true,
  suppressScrollLoad: false
};
const contentEl = document.getElementById("content");
const groupsEl = document.getElementById("groups");
const listEl = document.getElementById("list");
const detailEl = document.getElementById("detail");
const detailToggleEl = document.getElementById("detailToggle");
const searchEl = document.getElementById("search");
const currentGroupEl = document.getElementById("currentGroup");
const articleCountEl = document.getElementById("articleCount");
const feedFormEl = document.getElementById("feedForm");
const feedURLEl = document.getElementById("feedURL");
const feedListEl = document.getElementById("feedList");
const catalogGenreEl = document.getElementById("catalogGenre");
const catalogLanguageEl = document.getElementById("catalogLanguage");
const catalogResultsEl = document.getElementById("catalogResults");
const catalogAddEl = document.getElementById("catalogAdd");
let localFeeds = [];
let catalogFeeds = [];

function escapeHTML(value) {
  return String(value || "").replace(/[&<>"']/g, ch => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"
  }[ch]));
}

function loadingView(message, options = {}) {
  const classes = "loading" + (options.compact ? " compact" : "");
  const spinner = options.active === false ? "" : '<span class="loading-spinner" aria-hidden="true"></span>';
  return '<div class="' + classes + '" role="status">' + spinner +
    '<span>' + escapeHTML(message) + '</span></div>';
}

async function load(options = {}) {
  const older = Boolean(options.older);
  if (state.loading || (older && !state.hasMore)) return;
  state.loading = true;
  if (older) renderList();
  const params = new URLSearchParams({
    group: state.group,
    offset: older ? String(state.articles.length) : "0"
  });
  if (state.query) params.set("q", state.query);
  try {
    const res = await fetch("/api/state?" + params.toString());
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    const previousHeight = listEl.scrollHeight;
    const previousTop = listEl.scrollTop;
    const incoming = data.articles || [];
    state.articles = older ? incoming.concat(state.articles) : incoming;
    state.hasMore = Boolean(data.hasMore);
    if (!state.selected && state.articles.length) state.selected = state.articles[state.articles.length - 1].id;
    if (!state.articles.some(a => a.id === state.selected)) {
      state.selected = state.articles.length ? state.articles[state.articles.length - 1].id : null;
    }
    state.groups = data.groups || [];
    renderGroups();
    renderList();
    renderDetail();
    if (older) {
      listEl.scrollTop = previousTop + (listEl.scrollHeight - previousHeight);
    } else {
      listEl.scrollTop = listEl.scrollHeight;
    }
  } finally {
    state.loading = false;
    renderList();
  }
}

function renderGroups() {
  groupsEl.innerHTML = state.groups.map(g => {
    const icon = g.id === "stock" ? "♥ " : "";
    return '<button class="group ' + (g.id === state.group ? "active" : "") + '" data-group="' + escapeHTML(g.id) + '">' +
      '<span class="group-name">' + icon + escapeHTML(g.name) + '</span>' +
      '<span class="group-count">' + g.count + '</span>' +
    '</button>';
  }).join("");
  const active = state.groups.find(g => g.id === state.group) || state.groups[0];
  currentGroupEl.textContent = active ? active.name : "All";
  articleCountEl.textContent = state.articles.length + (state.hasMore ? "+ articles" : " articles");
}

async function loadFeeds() {
  const res = await fetch("/api/feeds");
  if (!res.ok) throw new Error(await res.text());
  const feeds = await res.json();
  localFeeds = feeds;
  feedListEl.innerHTML = feeds.map(f =>
    '<div class="feed-row">' +
      '<span class="feed-label" title="' + escapeHTML(f.url) + '">' + escapeHTML(f.title || f.url) + '</span>' +
      '<button class="feed-remove" type="button" data-remove-feed="' + f.id + '" data-feed-label="' + escapeHTML(f.title || f.url) + '" aria-label="Remove feed">×</button>' +
    '</div>'
  ).join("");
}

async function loadCatalogGenres() {
  const res = await fetch("/api/catalog/genres");
  if (!res.ok) throw new Error(await res.text());
  const data = await res.json();
  catalogGenreEl.innerHTML = '<option value="">Browse catalog…</option>' +
    (data.genres || []).map(g => '<option value="' + escapeHTML(g.slug) + '">' +
      escapeHTML(g.labelJa || g.label_ja || g.label) + '</option>').join("");
}

async function loadCatalogFeeds() {
  if (!catalogGenreEl.value) {
    catalogFeeds = [];
    renderCatalogFeeds();
    return;
  }
  const params = new URLSearchParams({ genre: catalogGenreEl.value });
  if (catalogLanguageEl.value) params.set("language", catalogLanguageEl.value);
  const res = await fetch("/api/catalog/feeds?" + params.toString());
  if (!res.ok) throw new Error(await res.text());
  const data = await res.json();
  catalogFeeds = data.feeds || [];
  renderCatalogFeeds();
}

function renderCatalogFeeds() {
  const registered = new Set(localFeeds.map(f => f.url));
  catalogResultsEl.innerHTML = catalogFeeds.map((f, index) => {
    const exists = registered.has(f.url);
    return '<label class="catalog-feed">' +
      '<input type="checkbox" data-catalog-index="' + index + '" ' + (exists ? "checked disabled" : "") + '>' +
      '<span>' + escapeHTML(f.title) + '<small>' + escapeHTML(exists ? "Already added" : (f.description || f.url)) + '</small></span>' +
    '</label>';
  }).join("");
  catalogAddEl.hidden = !catalogFeeds.some(f => !registered.has(f.url));
}

catalogGenreEl.addEventListener("change", () => loadCatalogFeeds().catch(showError));
catalogLanguageEl.addEventListener("change", () => loadCatalogFeeds().catch(showError));
catalogAddEl.addEventListener("click", async () => {
  const selected = Array.from(catalogResultsEl.querySelectorAll("[data-catalog-index]:checked:not(:disabled)"))
    .map(input => catalogFeeds[Number(input.dataset.catalogIndex)]);
  if (!selected.length) return;
  catalogAddEl.disabled = true;
  try {
    for (const feed of selected) {
      const res = await fetch("/api/feeds", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ url: feed.url })
      });
      if (!res.ok) throw new Error(await res.text());
    }
    await loadFeeds();
    renderCatalogFeeds();
    await load();
  } catch (err) { showError(err); }
  finally { catalogAddEl.disabled = false; }
});

feedFormEl.addEventListener("submit", async event => {
  event.preventDefault();
  try {
    const res = await fetch("/api/feeds", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ url: feedURLEl.value.trim() })
    });
    if (!res.ok) throw new Error(await res.text());
    feedURLEl.value = "";
    await loadFeeds();
    await load();
  } catch (err) { showError(err); }
});

feedListEl.addEventListener("click", async event => {
  const button = event.target.closest("[data-remove-feed]");
  if (!button) return;
  if (!window.confirm('Remove "' + button.dataset.feedLabel + '" and its saved articles?')) return;
  try {
    const res = await fetch("/api/feeds/" + button.dataset.removeFeed, { method: "DELETE" });
    if (!res.ok) throw new Error(await res.text());
    state.selected = null;
    await loadFeeds();
    await load();
  } catch (err) { showError(err); }
});

function renderList() {
  if (!state.articles.length) {
    listEl.innerHTML = state.loading
      ? loadingView("Loading articles...")
      : '<div class="empty">No articles</div>';
    return;
  }
  const loader = state.hasMore
    ? '<div class="loading compact" role="status">' +
        (state.loading
          ? '<span class="loading-spinner" aria-hidden="true"></span><span>Loading older articles...</span>'
          : '<button class="load-more" data-load-older type="button">Load older articles</button>') +
      '</div>'
    : "";
  listEl.innerHTML = loader + state.articles.map(a =>
    '<button class="article ' + (a.id === state.selected ? "active" : "") + ' ' + (a.isRead ? "read" : "") + '" data-id="' + a.id + '">' +
      '<div class="article-body">' +
      '<div class="article-top">' +
        '<span class="stock ' + (a.isStocked ? "on" : "") + '" data-stock-id="' + a.id + '" role="button" tabindex="-1" aria-label="' + (a.isStocked ? "Unstock" : "Stock") + '">♥</span>' +
        '<span class="article-title">' + escapeHTML(a.title) + '</span>' +
      '</div>' +
      '<div class="article-meta">' + escapeHTML(a.feedTitle) + ' · ' + escapeHTML(a.age) + '</div>' +
      (a.summary ? '<div class="article-summary">' + escapeHTML(a.summary) + '</div>' : "") +
      '</div>' +
      '<div class="article-media">' + articleMedia(a) + '</div>' +
    '</button>'
  ).join("");
}

function articleMedia(a) {
  if (a.imageUrl) {
    return '<img class="article-thumb" src="' + escapeHTML(a.imageUrl) + '" alt="" loading="lazy">';
  }
  return '<span class="article-placeholder" aria-hidden="true">' + escapeHTML(feedInitial(a.feedTitle || a.title)) + '</span>';
}

function feedInitial(value) {
  const trimmed = String(value || "tf").trim();
  return (Array.from(trimmed)[0] || "t").toUpperCase();
}

function renderDetail() {
  contentEl.classList.toggle("detail-closed", !state.detailOpen);
  detailToggleEl.textContent = state.detailOpen ? "Hide detail" : "Show detail";
  detailToggleEl.setAttribute("aria-expanded", String(state.detailOpen));
  const a = state.articles.find(item => item.id === state.selected);
  if (!a) {
    detailEl.innerHTML =
      '<button class="action detail-close" data-close-detail type="button" aria-label="Close detail">Close</button>' +
      '<div class="empty">No article selected</div>';
    return;
  }
  detailEl.innerHTML =
    '<button class="action detail-close" data-close-detail type="button" aria-label="Close detail">Close</button>' +
    '<h2>' + escapeHTML(a.title) + '</h2>' +
    '<div class="detail-meta">' + escapeHTML(a.feedTitle) + ' · ' + escapeHTML(a.age) + '</div>' +
    '<div class="detail-summary">' + escapeHTML(a.summary || "") + '</div>' +
    '<div class="actions">' +
      (a.link ? '<a class="action primary" href="' + escapeHTML(a.link) + '" target="_blank" rel="noreferrer">Open</a>' : "") +
      '<button class="action" id="stockButton">' + (a.isStocked ? "Unstock" : "Stock") + '</button>' +
    '</div>';
  fetch("/api/articles/" + a.id + "/read", { method: "POST" }).then(() => { a.isRead = true; renderList(); });
}

groupsEl.addEventListener("click", event => {
  const button = event.target.closest("[data-group]");
  if (!button) return;
  state.group = button.dataset.group;
  state.selected = null;
  state.hasMore = false;
  load();
});

listEl.addEventListener("click", event => {
  const loadOlder = event.target.closest("[data-load-older]");
  if (loadOlder) {
    load({ older: true }).catch(showError);
    return;
  }
  const stock = event.target.closest("[data-stock-id]");
  if (stock) {
    event.preventDefault();
    event.stopPropagation();
    const a = state.articles.find(item => item.id === Number(stock.dataset.stockId));
    if (a) toggleArticleStock(a).catch(showError);
    return;
  }
  const button = event.target.closest("[data-id]");
  if (!button) return;
  state.selected = Number(button.dataset.id);
  renderList();
  renderDetail();
  const a = state.articles.find(item => item.id === state.selected);
  if (a && a.link) window.open(a.link, "_blank", "noreferrer");
});

listEl.addEventListener("scroll", () => {
  if (!state.suppressScrollLoad && listEl.scrollTop <= 80) {
    load({ older: true }).catch(showError);
  }
});

detailEl.addEventListener("click", async event => {
  if (event.target.closest("[data-close-detail]")) {
    setDetailOpen(false);
    return;
  }
  if (event.target.id !== "stockButton") return;
  const a = state.articles.find(item => item.id === state.selected);
  if (!a) return;
  await toggleArticleStock(a);
});

detailToggleEl.addEventListener("click", () => {
  setDetailOpen(!state.detailOpen);
});

let searchTimer = 0;
searchEl.addEventListener("input", () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    state.query = searchEl.value.trim();
    state.selected = null;
    state.hasMore = false;
    load();
  }, 160);
});

function selectedIndex() {
  return state.articles.findIndex(a => a.id === state.selected);
}

function selectIndex(index) {
  if (!state.articles.length) return;
  const next = Math.max(0, Math.min(index, state.articles.length - 1));
  state.selected = state.articles[next].id;
  renderList();
  renderDetail();
  centerSelectedArticle(next);
}

function centerSelectedArticle(index) {
  const selected = listEl.querySelector('[data-id="' + state.selected + '"]');
  if (!selected) return;
  state.suppressScrollLoad = true;
  const selectedTop = selected.offsetTop;
  const selectedMiddle = selectedTop + selected.offsetHeight / 2;
  const targetTop = selectedMiddle - listEl.clientHeight / 2;
  const maxTop = listEl.scrollHeight - listEl.clientHeight;
  if (index <= 0) {
    listEl.scrollTop = 0;
  } else if (index >= state.articles.length - 1) {
    listEl.scrollTop = maxTop;
  } else {
    listEl.scrollTop = Math.max(0, Math.min(targetTop, maxTop));
  }
  requestAnimationFrame(() => {
    requestAnimationFrame(() => { state.suppressScrollLoad = false; });
  });
}

function setDetailOpen(open) {
  state.detailOpen = open;
  renderDetail();
}

function openSelected() {
  const a = state.articles.find(item => item.id === state.selected);
  if (a && a.link) window.open(a.link, "_blank", "noreferrer");
}

async function toggleSelectedStock() {
  const a = state.articles.find(item => item.id === state.selected);
  if (!a) return;
  await toggleArticleStock(a);
}

async function toggleArticleStock(a) {
  const res = await fetch("/api/articles/" + a.id + "/stock", { method: "POST" });
  if (!res.ok) throw new Error(await res.text());
  const wasStocked = a.isStocked;
  a.isStocked = !a.isStocked;
  updateStockCount(wasStocked ? -1 : 1);
  if (state.group === "stock" && !a.isStocked) {
    state.articles = state.articles.filter(item => item.id !== a.id);
    if (state.selected === a.id) {
      state.selected = state.articles.length ? state.articles[state.articles.length - 1].id : null;
    }
  }
  renderList();
  renderDetail();
}

function updateStockCount(delta) {
  const group = state.groups.find(g => g.id === "stock");
  if (!group) return;
  group.count = Math.max(0, Number(group.count || 0) + delta);
  renderGroups();
}

document.addEventListener("keydown", event => {
  const editing = event.target.matches("input, textarea, [contenteditable=true]");
  if (editing) {
    if (event.key === "Escape") {
      event.target.blur();
      event.preventDefault();
    }
    return;
  }
  if (event.key !== "g") state.pendingG = false;
  switch (event.key) {
  case "ArrowDown":
  case "j":
    event.preventDefault();
    selectIndex(selectedIndex() + 1);
    break;
  case "ArrowUp":
  case "k":
    event.preventDefault();
    selectIndex(selectedIndex() - 1);
    break;
  case "g":
    if (state.pendingG) {
      state.pendingG = false;
      selectIndex(0);
    } else {
      state.pendingG = true;
      setTimeout(() => { state.pendingG = false; }, 500);
    }
    break;
  case "G":
    selectIndex(state.articles.length - 1);
    break;
  case "h":
    setDetailOpen(false);
    break;
  case "l":
    setDetailOpen(true);
    break;
  case "o":
  case "Enter":
    openSelected();
    break;
  case " ":
    event.preventDefault();
    toggleSelectedStock().catch(showError);
    break;
  case "/":
    event.preventDefault();
    searchEl.focus();
    searchEl.select();
    break;
  default:
    return;
  }
});

function showError(err) {
  const message = err && err.message === "Failed to fetch"
    ? "Cannot reach tailfeed. Check that the tailfeed process is still running."
    : (err.message || String(err));
  window.alert(message);
}

load().catch(showError);
loadFeeds().catch(showError);
loadCatalogGenres().catch(err => {
  catalogGenreEl.innerHTML = '<option value="">Catalog unavailable</option>';
  catalogGenreEl.title = err.message || String(err);
});
</script>
</body>
</html>
`
