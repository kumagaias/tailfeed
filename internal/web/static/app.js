const pageSize = 50;
const state = {
  group: "all", query: "", articles: [], selected: null,
  groups: [], hasMore: false, loading: false, pendingG: false, detailOpen: true,
  suppressScrollLoad: false, view: "articles"
};
const articleViewEl = document.getElementById("articleView");
const settingsViewEl = document.getElementById("settingsView");
const settingsButtonEl = document.getElementById("settingsButton");
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
  const previousHeight = older ? listEl.scrollHeight : 0;
  const previousTop = older ? listEl.scrollTop : 0;
  state.loading = true;
  if (older) renderList();
  const params = new URLSearchParams({
    group: state.group,
    offset: older ? String(state.articles.length) : "0"
  });
  if (state.query) params.set("q", state.query);
  let loaded = false;
  try {
    const res = await fetch("/api/state?" + params.toString());
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    const incoming = data.articles || [];
    state.articles = older ? incoming.concat(state.articles) : incoming;
    state.hasMore = Boolean(data.hasMore);
    if (!state.selected && state.articles.length) state.selected = state.articles[state.articles.length - 1].id;
    if (!state.articles.some(a => a.id === state.selected)) {
      state.selected = state.articles.length ? state.articles[state.articles.length - 1].id : null;
    }
    state.groups = data.groups || [];
    renderGroups();
    renderDetail();
    loaded = true;
  } finally {
    state.loading = false;
    renderList();
    if (loaded) {
      const targetTop = older
        ? previousTop + (listEl.scrollHeight - previousHeight)
        : listEl.scrollHeight;
      restoreListScroll(targetTop);
    }
  }
}

function restoreListScroll(top) {
  state.suppressScrollLoad = true;
  listEl.scrollTop = Math.max(0, top);
  requestAnimationFrame(() => {
    requestAnimationFrame(() => { state.suppressScrollLoad = false; });
  });
}

function renderListPreservingScroll() {
  const top = listEl.scrollTop;
  renderList();
  restoreListScroll(top);
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
  feedListEl.innerHTML = feeds.map(f =>
    '<div class="feed-row">' +
      '<span class="feed-label" title="' + escapeHTML(f.url) + '">' + escapeHTML(f.title || f.url) + '</span>' +
      '<button class="feed-remove" type="button" data-remove-feed="' + f.id + '" data-feed-label="' + escapeHTML(f.title || f.url) + '" aria-label="Remove feed">×</button>' +
    '</div>'
  ).join("");
}

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
  fetch("/api/articles/" + a.id + "/read", { method: "POST" }).then(() => {
    if (!state.articles.some(item => item.id === a.id)) return;
    a.isRead = true;
    renderListPreservingScroll();
  });
}

groupsEl.addEventListener("click", event => {
  const button = event.target.closest("[data-group]");
  if (!button) return;
  showArticleView();
  state.group = button.dataset.group;
  state.selected = null;
  state.hasMore = false;
  load();
});

settingsButtonEl.addEventListener("click", () => {
  state.view = "settings";
  articleViewEl.hidden = true;
  settingsViewEl.hidden = false;
  settingsButtonEl.classList.add("active");
  });

function showArticleView() {
  state.view = "articles";
  articleViewEl.hidden = false;
  settingsViewEl.hidden = true;
  settingsButtonEl.classList.remove("active");
}

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
  if (!state.suppressScrollLoad && !state.loading && state.hasMore && listEl.scrollTop <= 80) {
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
  if (state.view === "settings") {
    if (event.key === "Escape") showArticleView();
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
