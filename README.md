# tailfeed

エンジニア向けのターミナル RSS リーダー。`tail -f` スタイルで記事を流し読み。

```
tailfeed  81 articles    ↑↓/jk move  ←→/hl detail  space ♥  G/gg  [ ] groups  / cmd  q quit
 All   ♥ Stock                                                          🔍 search...
────────────────────────────────────────────────────────────────────────────────────────────
▶❤ Why Rust is eating C++ — a deep dive into memory safety
  Hacker News  ·  2h ago
```

## インストール

```bash
brew install kumagaias/tap/tailfeed
```

## 使い方

```bash
tailfeed                  # TUI を開く
tailfeed -f               # 新着記事をストリーム表示
tailfeed summary today    # 今日の記事を AI 要約 → HTML レポート
tailfeed add <url>        # フィードを登録
tailfeed remove <url>     # フィードを削除
tailfeed list             # フィード一覧
```

### キーバインド

| キー | 動作 |
|------|------|
| `↑↓` / `jk` | カーソル移動 |
| `←→` / `hl` | 詳細ペイン開閉 |
| `space` | ♥ ストック切り替え |
| `G` / `gg` | 最新 / 最古 |
| `[ ]` / `Shift+←→` | グループ切り替え |
| `o` / `enter` | ブラウザで開く |
| `/` | コマンドパレット |
| `q` | 終了 |

### コマンド (`/`)

```
add <url>          フィード登録
remove <url>       フィード削除
find <keyword>     記事フィルタ
list               フィード管理
group new <name>   グループ作成
group del [name]   グループ削除
suggest            AI によるフィード提案 (MCP 必須)
summary            現在の記事を AI 要約 (MCP 必須)
summary today      今日の記事を要約 + HTML レポート
```

## MCP (AI 機能)

```bash
tailfeed mcp set <command> [args...]

# 例: Claude
tailfeed mcp set npx -y @anthropic/mcp-server-claude

# 要約言語を変更 (デフォルト: Japanese)
tailfeed mcp set <command> --language English
```

## データ

SQLite: `~/.local/share/tailfeed/feeds.db` (XDG) または `~/.tailfeed/feeds.db`

---

A terminal RSS reader for engineers. Scroll through articles `tail -f` style.

## Install

```bash
brew install kumagaias/tap/tailfeed
```

## Usage

```bash
tailfeed                  # open TUI
tailfeed -f               # stream new articles
tailfeed summary today    # AI summary of today's articles → HTML report
tailfeed add <url>        # subscribe
tailfeed remove <url>     # unsubscribe
tailfeed list             # list feeds
```

### Keybindings

| Key | Action |
|-----|--------|
| `↑↓` / `jk` | move cursor |
| `←→` / `hl` | open/close detail pane |
| `space` | toggle ♥ stock |
| `G` / `gg` | newest / oldest |
| `[ ]` / `Shift+←→` | switch group |
| `o` / `enter` | open in browser |
| `/` | command palette |
| `q` | quit |

### Commands (`/`)

```
add <url>          subscribe
remove <url>       unsubscribe
find <keyword>     filter articles
list               manage feeds
group new <name>   create group
group del [name]   delete group
suggest            AI feed suggestions (MCP required)
summary            summarise current article (MCP required)
summary today      summarise today's articles + HTML report
```

## MCP (AI features)

```bash
tailfeed mcp set <command> [args...]

# Example: Claude
tailfeed mcp set npx -y @anthropic/mcp-server-claude

# Set summary language (default: Japanese)
tailfeed mcp set <command> --language English
```

## Data

SQLite: `~/.local/share/tailfeed/feeds.db` (XDG) or `~/.tailfeed/feeds.db`

## License

MIT
