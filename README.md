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
tailfeed                  # open local browser UI at http://127.0.0.1:8080
tailfeed -t               # open TUI
tailfeed web              # open local browser UI explicitly
tailfeed -f               # stream new articles
tailfeed summary today    # AI summary of the last 24h → HTML report
tailfeed summary language Japanese
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

The browser UI supports `j`/`k` to move, `gg`/`G` for oldest/newest,
`h`/`l` to hide/show detail, `o`/`enter` to open, `space` to stock,
and `/` to focus search.

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
summary today      summarise articles from the last 24h + HTML report
summary language   set/show summary language
```

## MCP (AI features)

```bash
tailfeed mcp set <command> [args...]

# Example: Claude
tailfeed mcp set npx -y @anthropic/mcp-server-claude

# Set summary language (default: Japanese)
tailfeed summary language English
```

## Data

SQLite: `~/.local/share/tailfeed/feeds.db` (XDG) or `~/.tailfeed/feeds.db`

## License

MIT
