# Changelog

All notable changes to the Doggo VS Code extension will be documented in this file.

## [0.1.0] - 2026-02-25

### Added

- Doggo language registration for `*.dog`
- LSP integration (`doggo exec -- lsp`) with diagnostics, completion, definition, hover, and document symbols
- Commands:
  - `Doggo: Restart Language Server`
  - `Doggo: Show Language Server Output`
  - `Doggo: Run Selection in REPL`
  - `Doggo: Diagnose Indentation`
- `Ctrl+Enter` / `Cmd+Enter` keybinding for REPL selection execution
- TextMate grammar based syntax highlighting

### Changed

- Refactored extension activation to orchestrate service modules (LSP, REPL, indentation, command wiring)
- Switched packaging to an `esbuild` single-file bundle for cleaner VSIX contents
