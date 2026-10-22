# Security policy

Doggo is under active development. Use the latest patched release and a Go toolchain that satisfies the minimum version in `go.mod`.

Source and release builds are verified with Go 1.26.10, which includes the `crypto/x509` fix for [GO-2026-6809](https://pkg.go.dev/vuln/GO-2026-6809). For the Go 1.27 series the corresponding fix is 1.27.3; always use the latest patched version of a supported series. Rebuild existing binaries with a patched toolchain; updating source files alone does not patch an already-built runtime.

Doggo programs can access files, start processes, use the network, and call registered Go packages. Run programs and project dependencies only when you trust their source. The language runtime is not a sandbox for untrusted programs.

## Reporting vulnerabilities

Use GitHub's private vulnerability reporting feature on the public repository's Security tab. If that feature is unavailable, open an issue requesting a private reporting channel without including exploit details, credentials, or private data.

Include the affected Doggo version, operating system, Go version, expected behavior, and a minimal reproduction. Never submit production credentials or private signing keys.

## Release checks

CI runs dependency vulnerability checks for every distributed OS and architecture, checks Git history for credentials, and verifies the notices shipped with artifacts. A release must pass those checks and its native platform tests. Vulnerability checks use a pinned scanner version and the current advisory database.

Credential scanning runs independently of commit-identity policy checks. The strict identity policy described in [PUBLICATION.md](PUBLICATION.md) checks complete reachable commit history without legacy exceptions. It does not suppress or exempt credential scanning.

CLI archives and the VS Code extension include third-party licenses. Release bundles include checksums, SBOM files, a signature, and a public verification key. Verify the key fingerprint against a separately trusted copy before using a release.
