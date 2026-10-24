# Preparing public source

Prepare a separate public repository so the original development repository and its history remain available privately. Only committed changes are exported.

Run the exporter from a reviewed, tested commit:

```bash
python .github/scripts/prepare-public-source.py ../doggo-public
```

The destination must be new and outside the source repository. The exporter copies source, tests, examples, licenses and tooling, retains the documents listed in `docs/public-documents.txt` plus release governance and package readmes, and checks their local links. Internal design documents and local AI settings are excluded. The new repository has one initial commit with a GitHub noreply identity. License attribution is preserved.

Before contributing to the public repository, set a GitHub noreply identity in its local Git configuration. Use the noreply address shown by your GitHub account:

```bash
git config --local user.email YOUR_NOREPLY_ADDRESS
```

CI checks raw author and committer identities across the complete reachable history of the submitted head, including actual merge commits. Both identities must use a GitHub noreply address. Pull requests use the event's submitted head, excluding GitHub's synthetic test merge; integration tests still run on that merge. Historical violations inherited from the default branch or a PR base are rejected. Missing required commits or refs fail the check. Diagnostics identify the commit and role without repeating the email. Check account email choices when making commits through GitHub as well as local Git.

The `secret-scan` job scans all fetched Git history with gitleaks independently of `commit-identities`. An identity failure cannot skip credential scanning. The existing required `secrets` check remains an aggregate gate: it runs even if either job fails or is skipped, and succeeds only when both succeed. Existing branch protection for `secrets` therefore continues to require both policies without a settings change.

The release workflow strictly checks the selected tag's complete history before publication, independently of credential scanning. Scheduled and manual CI runs also audit the complete checked-out history. There are no legacy identity exceptions or baseline allowances. Run the same strict check locally:

```bash
python .github/scripts/check_commit_identity.py
```

Correcting existing identity metadata requires a separately reviewed history rewrite; adding a new commit does not remove old email values. Before rewriting, preserve a recoverable private backup and verify each email-to-account mapping. Replace only verified identities, preserve names, dates, messages, source trees and branch topology, and validate every affected ref. Preserve valid signatures on unchanged commits; signatures on rewritten commits cannot remain valid. Keep backups containing the old metadata private. Remote force pushes, tags and branch protection changes require a separate maintainer decision. Old clones and GitHub-retained copies can still contain the original history after a branch rewrite.

Run the full release checks in the exported repository before publishing it. Publishing the source, changing repository visibility, creating remote tags and uploading releases are separate maintainer operations.

When creating the public GitHub repository, enable private vulnerability reporting and protect its default branch with the CI checks. Configure the release signing keys as repository secrets and verify their public fingerprint through a trusted channel. Require successful native Linux, macOS and Windows checks before creating a release tag. Any GitHub Actions billing or quota restriction must be resolved before relying on hosted checks.
