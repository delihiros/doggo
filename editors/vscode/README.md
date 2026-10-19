# Doggo VS Code Extension

Doggo extension for Visual Studio Code.
It launches `doggo exec -- lsp` for `*.dog` files and provides REPL execution helpers.

## Compatibility policy

The extension supports VS Code 1.90.0 and later in the 1.x series. Keep
`vscode-languageclient` on 9.x while that floor is supported: client 10.1.2
requires VS Code 1.91.0 and rejects older hosts at runtime. Compile against an
exact `@types/vscode` version from the 1.90.x API line, so newer editor APIs
cannot accidentally enter the extension.

`npm run verify:compatibility` checks the locked runtime dependencies against
the entire declared editor range and checks the API types pin. Compilation and
bundling run this check, including during VSIX packaging. Dependabot continues
to propose language-client patch/minor updates and VS Code types patch updates;
major client upgrades and newer API lines require a deliberate migration.
These metadata and compilation checks do not verify activation in a real VS
Code host or communication with Doggo. Before claiming end-to-end compatibility,
validate language features, server restart, REPL execution and indentation in
supported editor versions, operating systems and remote extension hosts. Review
security updates separately; retaining client 9.x does not guarantee future
security coverage.

## Features

1. LSP integration via stdio (`doggo exec -- lsp`)
2. Diagnostics, completion, definition, hover, references, rename, document/workspace symbols
3. Semantic tokens and code actions (import fix / unused definition cleanup)
4. Syntax highlighting via TextMate grammar (`source.doggo`)
5. Auto indentation diagnostics for Doggo forms
6. Commands:
   1. `Doggo: Restart Language Server`
   2. `Doggo: Show Language Server Output`
   3. `Doggo: Run Selection in REPL`
   4. `Doggo: Diagnose Indentation`
7. Execute selected Doggo code in REPL via `Ctrl+Enter` / `Cmd+Enter`

## Settings

1. `doggo.lsp.command`
2. `doggo.lsp.args`
3. `doggo.lsp.env`
4. `doggo.trace.server`
5. `doggo.debug.indentTrace`

When `doggo.lsp.command` is empty, the extension runs `doggo exec -- lsp` from `PATH`.
Run `doggo build` first. REPL execution runs `doggo exec -- repl` and holds the same generation lease through the Doggo CLI.
An explicit `doggo.lsp.command` runs that command directly with the `lsp` or `repl` subcommand.

## Local Development

Use Node.js 22.12.0 or later for extension development and release tooling.
VSCE 4 loads ES modules from CommonJS, which requires the default `require(esm)`
support added in Node 22.12.0. The repository's `.nvmrc` selects the Node version
used by CI. Dependency installation enforces this minimum through `.npmrc`; the
test, verify, and package commands also check it explicitly.

```bash
cd editors/vscode
npm ci
npm run test
npm run bundle
```

Package VSIX:

```bash
npm run package
```

Release gate checks:

```bash
npm run verify
npm audit
npm audit --omit=dev
go test ./internal/lsp ./internal/cli
```

The bundle includes runtime dependency notices in `THIRD_PARTY_NOTICES`.
`npm run licenses:check` checks the notices against the dependencies actually
included by esbuild. `npm run package` creates `dist/doggo-vscode.vsix` on Windows,
macOS, and Linux.

## Architecture (Refactored)

1. `src/extension.ts`: activation orchestration only
2. `src/lsp/clientManager.ts`: LSP client lifecycle and start single-flight
3. `src/lsp/runtime.ts`: VS Code + `vscode-languageclient` runtime binding
4. `src/repl/replTerminalManager.ts`: REPL terminal lifecycle and command resolution
5. `src/config/settings.ts`: normalized settings parsing
6. `src/commands/registerCommands.ts`: command registration wiring
7. `src/indent/indentController.ts`: indentation diagnostics and trace logging

## Indentation Troubleshooting

If Enter does not indent:

1. Run `Doggo: Diagnose Indentation`.
2. Open `Doggo Language Server` output and verify:
   1. `editor.autoIndent` is not `none`
   2. `rule.increaseIndent` or `rule.onEnterMatches` is true at cursor line
   3. closing delimiters (`)`, `]`, `}`) match `outdent` where expected
3. Enable `doggo.debug.indentTrace` and press Enter again to inspect newline traces.

## Syntax Highlighting

The extension provides TextMate scopes and lets your theme decide colors.
If you need custom colors, use `editor.tokenColorCustomizations`.

Recommended role-based palette:

1. Syntax keywords
2. Function and macro call heads
3. Literals (`string` / `number` / keyword literal)
4. Comments
5. Delimiters (`()`, `[]`, `{}`)

## Marketplace Status

This repository contains Marketplace-ready packaging metadata and VSIX generation.
Actual Marketplace publish (`vsce publish`) is intentionally out of scope for this phase.
