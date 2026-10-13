# Preparing public source

Prepare a separate public repository so the original development repository and its history remain available privately. Only committed changes are exported.

Run the exporter from a reviewed, tested commit:

```bash
python .github/scripts/prepare-public-source.py ../doggo-public
```

The destination must be new and outside the source repository. The exporter copies source, tests, examples, licenses and tooling, retains the documents listed in `docs/public-documents.txt` plus release governance and package readmes, and checks their local links. Internal design documents and local AI settings are excluded. The new repository has one initial commit with a GitHub noreply identity. License attribution is preserved.

Run the full release checks in the exported repository before publishing it. Publishing the source, changing repository visibility, creating remote tags and uploading releases are separate maintainer operations.

When creating the public GitHub repository, enable private vulnerability reporting and protect its default branch with the CI checks. Configure the release signing keys as repository secrets and verify their public fingerprint through a trusted channel. Require successful native Linux, macOS and Windows checks before creating a release tag. Any GitHub Actions billing or quota restriction must be resolved before relying on hosted checks.
