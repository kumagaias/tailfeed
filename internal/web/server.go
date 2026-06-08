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
	db *db.DB
}

// New creates a browser UI server.
func New(database *db.DB) *Server {
	return &Server{db: database}
}

// ListenAndServe starts the browser UI on addr.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/articles/", s.handleArticleAction)

	server := &http.Server{
		Addr:              addr,
		Handler:           logRequest(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
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
  color-scheme: light;
  --bg: #f6f7f9;
  --panel: #ffffff;
  --text: #15171a;
  --muted: #69707a;
  --line: #d9dee5;
  --accent: #0f766e;
  --accent-strong: #0b5f59;
  --read: #7c8591;
  --stock: #c0264e;
  --shadow: 0 1px 2px rgba(18, 24, 31, .08);
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
  background: #111418;
  color: #f7f8fa;
  padding: 18px 12px;
  border-right: 1px solid #0b0d10;
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
.group {
  width: 100%;
  border: 0;
  background: transparent;
  color: #c6ccd4;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: center;
  text-align: left;
  padding: 9px 10px;
  border-radius: 6px;
  cursor: pointer;
}
.group:hover { background: #1c2229; color: white; }
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
  background: #fbfcfd;
  color: var(--text);
}
.content {
  display: grid;
  grid-template-columns: minmax(360px, 44%) minmax(0, 1fr);
  min-height: 0;
  overflow: hidden;
}
.content.detail-closed { grid-template-columns: minmax(0, 1fr); }
.content.detail-closed .list { border-right: 0; }
.content.detail-closed .detail { display: none; }
.list {
  min-height: 0;
  overflow: auto;
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
  grid-template-columns: minmax(0, 1fr);
  gap: 10px;
}
.article.has-image {
  grid-template-columns: 96px minmax(0, 1fr);
}
.article:hover { border-color: #b7c0ca; }
.article.active { border-color: var(--accent); box-shadow: 0 0 0 2px rgba(15, 118, 110, .14); }
.article.read .article-title { color: var(--read); font-weight: 500; }
.article-thumb {
  width: 96px;
  height: 72px;
  object-fit: cover;
  border-radius: 6px;
  border: 1px solid var(--line);
  background: #eef1f4;
}
.article-top {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.stock {
  color: #aab2bd;
  flex: 0 0 auto;
}
.stock.on { color: var(--stock); }
.article-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-weight: 700;
  color: var(--text);
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
  color: #424951;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.detail {
  position: relative;
  min-height: 0;
  overflow: auto;
  padding: 28px 34px;
  background: #fbfcfd;
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
  display: block;
  width: min(100%, 380px);
  max-height: 190px;
  object-fit: cover;
  border-radius: 8px;
  border: 1px solid var(--line);
  margin-bottom: 22px;
  background: #eef1f4;
}
.detail-summary {
  white-space: pre-wrap;
  color: #252a30;
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
  .list { max-height: 60vh; border-right: 0; }
  .detail { border-top: 1px solid var(--line); }
  .article.has-image { grid-template-columns: 82px minmax(0, 1fr); }
  .article-thumb { width: 82px; height: 62px; }
}
</style>
</head>
<body>
<div class="app">
  <aside class="sidebar">
    <div class="brand"><span class="brand-mark">tf</span><span>tailfeed</span></div>
    <nav id="groups" class="groups"></nav>
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
  hasMore: false, loading: false, pendingG: false, detailOpen: true,
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
    renderGroups(data.groups || []);
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

function renderGroups(groups) {
  groupsEl.innerHTML = groups.map(g => {
    const icon = g.id === "stock" ? "♥ " : "";
    return '<button class="group ' + (g.id === state.group ? "active" : "") + '" data-group="' + escapeHTML(g.id) + '">' +
      '<span class="group-name">' + icon + escapeHTML(g.name) + '</span>' +
      '<span class="group-count">' + g.count + '</span>' +
    '</button>';
  }).join("");
  const active = groups.find(g => g.id === state.group) || groups[0];
  currentGroupEl.textContent = active ? active.name : "All";
  articleCountEl.textContent = state.articles.length + (state.hasMore ? "+ articles" : " articles");
}

function renderList() {
  if (!state.articles.length) {
    listEl.innerHTML = state.loading
      ? loadingView("Loading articles...")
      : '<div class="empty">No articles</div>';
    return;
  }
  const loader = state.hasMore
    ? loadingView(
        state.loading ? "Loading older articles..." : "Scroll up to load more",
        { compact: true, active: state.loading }
      )
    : "";
  listEl.innerHTML = loader + state.articles.map(a =>
    '<button class="article ' + (a.imageUrl ? "has-image " : "") + (a.id === state.selected ? "active" : "") + ' ' + (a.isRead ? "read" : "") + '" data-id="' + a.id + '">' +
      (a.imageUrl ? '<img class="article-thumb" src="' + escapeHTML(a.imageUrl) + '" alt="" loading="lazy">' : "") +
      '<div class="article-body">' +
      '<div class="article-top">' +
        '<span class="stock ' + (a.isStocked ? "on" : "") + '">♥</span>' +
        '<span class="article-title">' + escapeHTML(a.title) + '</span>' +
      '</div>' +
      '<div class="article-meta">' + escapeHTML(a.feedTitle) + ' · ' + escapeHTML(a.age) + '</div>' +
      (a.summary ? '<div class="article-summary">' + escapeHTML(a.summary) + '</div>' : "") +
      '</div>' +
    '</button>'
  ).join("");
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
    (a.imageUrl ? '<img class="detail-image" src="' + escapeHTML(a.imageUrl) + '" alt="">' : "") +
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
  await fetch("/api/articles/" + a.id + "/stock", { method: "POST" });
  a.isStocked = !a.isStocked;
  renderList();
  renderDetail();
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
  const selected = listEl.querySelector('[data-id="' + state.selected + '"]');
  if (selected) {
    state.suppressScrollLoad = true;
    selected.scrollIntoView({ block: "nearest" });
    requestAnimationFrame(() => {
      requestAnimationFrame(() => { state.suppressScrollLoad = false; });
    });
  }
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
  await fetch("/api/articles/" + a.id + "/stock", { method: "POST" });
  a.isStocked = !a.isStocked;
  renderList();
  renderDetail();
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
  case "j":
    selectIndex(selectedIndex() + 1);
    break;
  case "k":
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
  listEl.innerHTML = '<div class="empty">' + escapeHTML(err.message) + '</div>';
}

load().catch(showError);
</script>
</body>
</html>
`
