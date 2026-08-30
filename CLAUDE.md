# Claude Context: Go CLI Project

## Project Overview
* **Language:** Go (Golang) 1.22+
* **Type:** Command Line Interface (CLI) application
* **Primary Framework:** [spf13/cobra](https://github.com)
* **Configuration Management:** [spf13/viper](https://github.com)

## Directory Structure
* `/cmd`: Contains Cobra command definitions (e.g., `root.go`, `version.go`).
* `/pkg`: Reusable, isolated logic packages.
* `/internal`: Private application code specific to this CLI.
* `/main.go`: Entry point initializing and executing the root Cobra command.

## Architectural Rules
* Keep `cmd` files lightweight; delegate business logic to `/internal` or `/pkg`.
* Bind Cobra flags to Viper configuration within the `init()` functions.
* Return errors from functions instead of calling `log.Fatalf` inside packages.
* Handle all user-facing errors gracefully at the CLI surface layer (`cmd/`).

## Style & Patterns
* Use structured logging via `slog` for debug and verbose modes.
* Print standard output to `os.Stdout` and errors/logs to `os.Stderr`.
* Use Go channels and `context.Context` for timeouts and CLI interruptions (Ctrl+C).
* For any file update operation (move/delete file, change metadata etc) - always preview the file changes via TOON format and ask for confirmaton, unless user provided '-y' flag to skip confirmation explicitly 

## Testing Strategy
* Place unit tests alongside code as `*_test.go`.
* Use `bytes.Buffer` to capture and assert command output in CLI integration tests.

## Commit Messages
Follow Conventional Commits (Angular style):

```
type(scope): description

[optional body]

[optional footer(s)]
```

- **type**: lowercase, required — one of `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`, `ci`, `build`, `revert`.
- **scope**: lowercase, optional but recommended (e.g. `cli`, `hash`, `list`, `ffmpeg`).
- **description**: imperative mood, lowercase start, no trailing period, whole first line ≤ 72 chars. Say WHAT changed, not HOW.
- **body**: optional; explain rationale / context, wrap prose.
- Keep the existing footer trailers (`Co-Authored-By`, `Claude-Session`).

Example: `feat(set): write iTunes-style MP4 atoms so Windows reads them`

## Versioning Workflow
- **Update Command**: When I say "version update," update `CHANGELOG.md` and update `cmd/version.txt` with {version_number}+{last_9_characters_of_last_git_commit_id}.{last_commit_date_in_YYYY-MM-DD_format} 
- **Date Format**: Use YYYY-MM-DD.
- **Changelog Format**: Follow the [Keep a Changelog](https://keepachangelog.com/) standard.
- **Content**: Summarize all recent changes made in the current session.