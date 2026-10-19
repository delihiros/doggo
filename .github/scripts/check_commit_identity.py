"""Check raw commit identities without printing personal email addresses."""

import argparse
import os
from pathlib import Path
import re
import subprocess
import sys


NOREPLY = re.compile(r"(?:[0-9]+\+)?[A-Za-z0-9-]+(?:\[bot\])?@users\.noreply\.github\.com", re.IGNORECASE)


def resolve_commit(repository, revision):
    if not revision or revision.startswith("-"):
        raise ValueError("Invalid commit revision")
    return subprocess.check_output(
        ["git", "--no-replace-objects", "rev-parse", "--verify", f"{revision}^{{commit}}"], cwd=repository,
        stderr=subprocess.DEVNULL,
    ).decode("ascii").strip()


def check_identities(repository, revision="HEAD", base=None):
    head = resolve_commit(repository, revision)
    selected = f"{resolve_commit(repository, base)}..{head}" if base else head
    # Lowercase %ae/%ce deliberately inspect raw objects, without mailmap rewriting.
    records = subprocess.check_output(
        ["git", "--no-replace-objects", "log", "--no-show-signature", "--format=%H%x00%ae%x00%ce", selected], cwd=repository,
    ).decode("utf-8", errors="replace")
    issues = []
    for record in records.splitlines():
        commit, author, committer = record.split("\0")
        for role, email in [("author", author), ("committer", committer)]:
            if email.lower() != "noreply@github.com" and not NOREPLY.fullmatch(email):
                issues.append({"commit": commit, "role": role})
    return issues


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--revision", default="HEAD")
    parser.add_argument("--base", help="Check commits reachable from revision and not base; omit to check all reachable history")
    parser.add_argument("--ci", action="store_true", help="Use COMMIT_BASE for a push/PR range; other events check all history")
    args = parser.parse_args()
    base = args.base
    if args.ci:
        base = os.environ.get("COMMIT_BASE", "")
        if base and not re.fullmatch(r"[0-9a-fA-F]{40}|[0-9a-fA-F]{64}", base):
            parser.error("Invalid CI base commit")
        if not base or set(base) == {"0"}:
            base = None
        elif base:
            try:
                resolve_commit(args.repository, base)
            except subprocess.CalledProcessError:
                # A replaced branch may no longer expose the event's old head.
                # Checking all reachable history is stricter than checking a range.
                base = None
    try:
        issues = check_identities(args.repository, args.revision, base)
    except (ValueError, subprocess.CalledProcessError) as exc:
        print(f"Could not verify commit identities: {type(exc).__name__}", file=sys.stderr)
        return 2
    for issue in issues:
        print(f"{issue['commit']}: {issue['role']} must use a public noreply identity (email redacted)", file=sys.stderr)
    if issues:
        return 1
    print("Commit identities verified.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
