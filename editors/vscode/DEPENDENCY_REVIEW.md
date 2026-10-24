# VS Code dependency review — 2026-10-15

Reviewed main `beb9bbd`, PR #3 `fac078c`, PR #7 `c2360c6`, and the manifests
for PRs #4–#6. The existing extension tests exercise lifecycle, commands,
settings, syntax, indentation, dependency safety and licenses under Node;
they do not launch a VS Code extension host or the real Doggo LSP.

## Decision

Keep VS Code `^1.90.0` and languageclient `^9.0.1` (locked to 9.0.1).
Do not merge #3 or #5 as written. There is no identified extension feature or
currently reported audit finding requiring a host compatibility break.
Pin API types to 1.90.0, add a lockfile compatibility gate to compilation and
bundling, and suppress automatic version-update PRs that cross these API floors.
This PR is an alternative to #3, not an upgrade to languageclient 10.
Existing Dependabot PRs are not automatically closed by this configuration.
Security updates still need review; retaining 9.x is not a permanent security
guarantee.

| Approach | Benefit | Cost and required work |
| --- | --- | --- |
| Keep 1.90 / client 9.x | Preserve declared support; existing implementation compiles against 1.90 types; current audit reports zero vulnerabilities | Defer client 10 features/fixes; continue reviewing security and compatible updates |
| Raise to at least 1.91 / client 10.1.2 | Adopt newer client and protocol packages | Exclude 1.90 users; synchronize manifest/lock/types; change module resolution; migrate output channels and tests to LogOutputChannel; adapt glob tests; regenerate notices; validate real host and LSP behavior |

PR #3 changes client 9.0.1 to 10.1.2, protocol/types 3.17.5 to 3.18.4,
JSON-RPC 8.2.0 to 9.0.3, and minimatch 5.1.9 to 10.2.6. The installed client
source `lib/node/main.js` contains `REQUIRED_VSCODE_VERSION = '^1.91.0'` and
calls `checkVersion()` from the constructor. It throws for older hosts: the
manifest mismatch affects client construction, not just npm metadata.
In addition, minimatch 10 exports a namespace with a `minimatch` function;
the existing dependency safety tests assume the callable minimatch 5 export.

## Related PRs

| PR | Assessment |
| --- | --- |
| #3 languageclient 10.1.2 | Original type-check fails with TS2307 for `vscode-languageclient/node`. An exploratory `module: Node16` change then fails with TS2740: the existing OutputChannel is not a LogOutputChannel. Runtime also rejects 1.90. Defer. |
| #4 Node types 26.6.5 | Does not establish runtime support for Node 26 APIs. Development minimum is 22.12 and CI uses Node 24; the bundled extension runs on VS Code's own Node. Review runtime/API alignment separately; not adopted here. |
| #5 VS Code types 1.140.0 | Conceals newer-API usage against the 1.90 floor. Even main had resolved 1.109.0 through its caret range. Replace with the 1.90.0 pin here. |
| #6 esbuild 0.28.2 | Build-tool update, separate from the editor API floor. See validation below; not adopted here. |
| #7 TypeScript 7.0.2 | Isolated PR passes existing verification and VSIX packaging on Linux. It adds platform-specific compiler packages. Keep separate; Linux success does not verify Windows/macOS or optional-package installation on those systems. |

## Validation

Local environment: Linux x64, Node 24.19.0, npm 11.9.0. Dependencies installed
with `npm ci --ignore-scripts`; the subsequently invoked esbuild and compiler
binaries ran successfully. This does not verify dependency install hooks.

- Main: `npm run verify`, 43 tests passed; `npm audit`, zero findings.
- PR #3: reproduced type-check failures described above, first unmodified and
  then with only exploratory Node16 module resolution.
- PR #7: `npm run verify`, 43 tests passed; `npm run package` produced a VSIX.
- PR #6: `npm run verify`, 43 tests passed, including bundling and notices.
- This fix: `npm run verify`, 47 tests passed; `npm run package` produced a VSIX;
  `npm audit`, zero findings. Compilation uses VS Code 1.90.0 types.
- The new gate applied to actual PR #3 and #5 manifests/locks rejects both;
  tests also reject narrowed runtime support, floating/newer API types, and a
  stale root lockfile engine. A synchronized 1.91 fixture is accepted.

Not verified: real VS Code 1.90/1.91/current host activation, LSP handshake,
diagnostics/completion/rename/restart against Doggo, REPL and indentation in the
editor, Windows/macOS, remote extension hosts, the exact CI Node 24.21.0,
Marketplace installation or publishing, and the Go test suite. The local
`code` launcher reports no installed VS Code; no extension host is available.
PRs #4/#5 were reviewed through their diffs rather than independently built.
Before claiming end-to-end 1.90 compatibility, install the VSIX in VS Code
1.90, run `doggo build`, open a `.dog` workspace, and check the features above.

## Security follow-up — 2026-10-24

CI's `npm audit` now reports [GHSA-xcx6-jx8p-x3h5](https://github.com/AzureAD/microsoft-authentication-library-for-js/security/advisories/GHSA-xcx6-jx8p-x3h5)
through `@vscode/vsce@4.0.0 → @azure/identity@4.13.3 → @azure/msal-node@6.0.1`.
MSAL Node versions before 7.1.0 in this series are affected. The current
Azure Identity 4.14.0 still declares MSAL Node `^6.0.0`, so updating within
the parent's declared range cannot select the security fix.

Use an override scoped to Azure Identity's MSAL Node dependency, requiring
`^7.1.0` and locking 7.1.1 with MSAL Common 16.14.3. Remove the override once
Azure Identity's dependency range selects a patched release normally.
This dependency belongs to the VSIX packaging tool; the extension continues
to use languageclient 9.0.1 and VS Code 1.90 APIs. MSAL Node 7 retains the
Node 20 minimum. Its v7 interactive-authentication change removes the
`responseMode` option, which this Azure Identity version does not pass.

Validate with a fresh `npm ci`, `npm audit`, `npm run verify`, and
`npm run package`. Offline smoke checks also load Azure Identity and
construct its default and client-secret credentials. These checks cover
local tooling and packaging; Marketplace authentication needs a separate
maintainer validation when publishing.
