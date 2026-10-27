# Remove hosted API integration — 2026-10-28

## Goal
Remove tailfeed's hosted API dependency and keep AI summaries and feed suggestions available through MCP.

## Decisions
- Remove hosted API clients, credentials, usage and catalog reads, and register/suggest/summary API calls.
- Require a configured MCP server for AI summaries and feed suggestions.
- Keep the local browser UI endpoints used by the desktop app.
- Split summary formatting helpers into a separate file to keep source files under 500 lines.

## Changes
- Removed the hosted API package and summary fallback.
- Removed API usage and catalog commands/routes/UI.
- Added a command test for the removed register and usage commands, and web tests for removed catalog routes.
- Extracted browser UI assets and summary formatting helpers into separate files.

## Checks
- make test — gitleaks and go test ./... passed.

## Next steps
- Local merge to main is part of this task. Push and verify the automatic patch release and Homebrew update when publishing is requested.
