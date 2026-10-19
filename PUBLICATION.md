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

CI checks the raw author and committer identities of new push/PR commits, including merge commits. If a replaced branch's previous head is unavailable, it checks all reachable history instead. Both identities must use a GitHub noreply address; diagnostics identify the commit and role without repeating the email. Check account email choices when making commits through GitHub as well as local Git. To verify all history reachable from the current branch before publication, run:

```bash
python .github/scripts/check_commit_identity.py
```

A successful new-commit check does not sanitize earlier history. If personal identities are already public, preserve the original repository and prepare a separate corrected publication history for review. Changing remote history, tags, or branch protections requires a separate maintainer decision; adding another commit does not remove old identity metadata or copies retained by GitHub.

Run the full release checks in the exported repository before publishing it. Publishing the source, changing repository visibility, creating remote tags and uploading releases are separate maintainer operations.

When creating the public GitHub repository, enable private vulnerability reporting and protect its default branch with the CI checks. Configure the release signing keys as repository secrets and verify their public fingerprint through a trusted channel. Require successful native Linux, macOS and Windows checks before creating a release tag. Any GitHub Actions billing or quota restriction must be resolved before relying on hosted checks.
