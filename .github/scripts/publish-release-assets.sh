#!/usr/bin/env bash
set -euo pipefail

version="${1:?release version is required}"
bundle_dir="${2:?release bundle directory is required}"

assets=()
while IFS= read -r asset; do assets+=("$asset"); done < <(find "$bundle_dir" -maxdepth 1 -type f | LC_ALL=C sort)
test "${#assets[@]}" -gt 0
expected_assets="$(printf '%s\n' "${assets[@]##*/}" | LC_ALL=C sort -u)"

require_draft() {
  local draft
  draft="$(gh release view "$version" --json isDraft --jq '.isDraft')"
  if [[ "$draft" != true ]]; then
    echo "release must be a draft; refusing to replace published assets" >&2
    exit 1
  fi
}

if gh release view "$version" >/dev/null 2>&1; then
  require_draft
  existing_assets="$(gh release view "$version" --json assets --jq '.assets[].name' | LC_ALL=C sort -u)"
  stale_assets="$(comm -13 <(printf '%s\n' "$expected_assets") <(printf '%s\n' "$existing_assets"))"
  if [[ -n "$stale_assets" ]]; then
    echo "existing release contains stale assets:" >&2
    printf '%s\n' "$stale_assets" >&2
    exit 1
  fi
else
  gh release create "$version" --draft --verify-tag --generate-notes --title "Doggo $version"
fi

require_draft
# A failed upload remains private and can be retried while the release is a draft.
gh release upload "$version" --clobber "${assets[@]}"
uploaded_assets="$(gh release view "$version" --json assets --jq '.assets[].name' | LC_ALL=C sort)"
if [[ "$uploaded_assets" != "$expected_assets" ]]; then
  echo "uploaded release assets do not match the reviewed bundle" >&2
  exit 1
fi

# Inspect downloaded bytes as well as names before publishing. This also checks
# sizes without depending on optional server-side digest metadata.
verification_dir="$(mktemp -d)"
trap 'find "$verification_dir" -mindepth 1 -maxdepth 1 -type f -delete; rmdir -- "$verification_dir"' EXIT
gh release download "$version" --dir "$verification_dir"
downloaded=()
while IFS= read -r asset; do downloaded+=("${asset##*/}"); done < <(find "$verification_dir" -maxdepth 1 -type f | LC_ALL=C sort)
if [[ "$(printf '%s\n' "${downloaded[@]}")" != "$expected_assets" ]]; then
  echo "downloaded release assets do not match the reviewed bundle" >&2
  exit 1
fi
for asset in "${assets[@]}"; do
  if ! cmp -s -- "$asset" "$verification_dir/${asset##*/}"; then
    echo "downloaded asset differs from the reviewed bundle: ${asset##*/}" >&2
    exit 1
  fi
done
require_draft
gh release edit "$version" --draft=false
