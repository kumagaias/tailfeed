# tailfeed

A tail-style terminal RSS reader for engineers.

```
tailfeed  81 articles    ↑↓/jk move  ←→/hl detail  space ♥  G/gg  [ ] groups  / cmd  q quit
 All   ♥ Stock                                                          🔍 search...
────────────────────────────────────────────────────────────────────────────────────────────
▶❤ Typeless で音声入力や — タイピング不要のAI時代がきたで
  Zenn のトレンド  ·  3d ago
  AI音声入力アプリ Typeless の使い方を徹底解説するで。
```

## Install

```bash
brew install kumagaias/tap/tailfeed
```

## Usage

```bash
tailfeed                  # open TUI
tailfeed -f               # stream new articles (tail -f style)
tailfeed summary today    # summarise today's articles via MCP → HTML report
tailfeed add <url>        # subscribe to a feed
tailfeed remove <url>     # unsubscribe
tailfeed list             # list feeds
```

### TUI keybindings

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
add <url>              subscribe to feed
remove <url>           unsubscribe
find <keyword>         filter articles
list                   manage feeds
group new <name>       create group
group del [name]       delete group
suggest                suggest feeds via AI (MCP required)
summary                summarise current article (MCP required)
summary today          summarise today's articles + HTML report
```

## MCP (AI features)

tailfeed integrates with any MCP-compatible AI server for feed suggestions and article summaries.

```bash
# Register an MCP server
tailfeed mcp set <command> [args...]

# Example: Claude via mcp-server-claude
tailfeed mcp set npx -y @anthropic/mcp-server-claude

# Set summary language (default: Japanese)
tailfeed mcp set <command> --language English
```

## Data

SQLite database stored at `~/.local/share/tailfeed/feeds.db` (XDG) or `~/.tailfeed/feeds.db`.

## License

MIT
