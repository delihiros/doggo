#!/usr/bin/env bash
set -euo pipefail
if [[ "${GITHUB_EVENT_NAME}" == "workflow_dispatch" ]]; then
  release_ref="refs/tags/${REQUESTED_VERSION}"
else
  release_ref="${GITHUB_REF}"
fi
if [[ "$release_ref" != refs/tags/v* ]]; then
  echo "Release source must be an existing version tag" >&2
  exit 1
fi
tag_commit="$(git rev-parse --verify "${release_ref}^{commit}")"
head_commit="$(git rev-parse --verify HEAD)"
if [[ "$head_commit" != "$tag_commit" ]]; then
  echo "Checked-out source does not match the release tag commit" >&2
  exit 1
fi
if [[ -n "${EXPECTED_RELEASE_COMMIT:-}" && "$head_commit" != "$EXPECTED_RELEASE_COMMIT" ]]; then
  echo "Checked-out source differs from the resolved release commit" >&2
  exit 1
fi
printf '%s\n' "$head_commit"
