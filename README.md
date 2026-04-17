# tailfeed

A terminal RSS reader for engineers. Scroll through articles `tail -f` style.

```
tailfeed  81 articles    ↑↓/jk move  ←→/hl detail  space ♥  G/gg  [ ] groups  / cmd  q quit
 All   ♥ Stock                                                          🔍 search...
────────────────────────────────────────────────────────────────────────────────────────────
▶❤ Why Rust is eating C++ — a deep dive into memory safety
  Hacker News  ·  2h ago
```

## Install

```bash
brew install kumagaias/tap/tailfeed
```

## Usage

```bash
tailfeed                  # open TUI
tailfeed -f               # stream new articles
tailfeed summary today    # AI summary → HTML report
tailfeed add <url>        # subscribe
tailfeed add sample       # add popular feeds (--lang ja for Japanese)
tailfeed remove <url>     # unsubscribe
tailfeed list             # list feeds
```

## Keybindings

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

## Commands (`/`)

```
add <url>          subscribe
remove <url>       unsubscribe
find <keyword>     filter articles
list               manage feeds
group new <name>   create group
group del [name]   delete group
suggest            AI feed suggestions
summary            summarise current article
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
