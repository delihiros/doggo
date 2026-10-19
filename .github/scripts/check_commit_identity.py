"""Check raw commit identities without printing personal email addresses."""

import argparse
import json
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


def event_sha(value, allow_zero=False):
    if not isinstance(value, str) or not re.fullmatch(r"[0-9a-fA-F]{40}|[0-9a-fA-F]{64}", value):
        raise ValueError("Invalid event commit")
    if set(value) == {"0"}:
        if allow_zero:
            return None
        raise ValueError("Missing event commit")
    return value


def ci_range(repository):
    with open(os.environ["GITHUB_EVENT_PATH"], encoding="utf-8") as source:
        event = json.load(source)
    event_name = os.environ["GITHUB_EVENT_NAME"]
    if event_name == "pull_request":
        # HEAD is GitHub's synthetic test merge, not the submitted commit.
        pr = event["pull_request"]
        return event_sha(pr["head"]["sha"]), event_sha(pr["base"]["sha"])
    if event_name != "push":
        raise ValueError("Unsupported event; run without --ci for a full-history audit")
    head = event_sha(event["after"])
    base = event_sha(event["before"], allow_zero=True)
    default_branch = event["repository"]["default_branch"]
    if not isinstance(default_branch, str) or not default_branch:
        raise ValueError("Missing default branch")
    for ref in [f"refs/heads/{default_branch}", event["ref"]]:
        subprocess.check_call(["git", "check-ref-format", ref], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    if event["ref"].startswith("refs/heads/") and event["ref"] != f"refs/heads/{default_branch}":
        # A rebase can introduce existing main history relative to event.before.
        # fetch-depth: 0 provides the default branch; missing it must fail closed.
        return head, resolve_commit(repository, f"refs/remotes/origin/{default_branch}")
    if base:
        try:
            resolve_commit(repository, base)
        except subprocess.CalledProcessError:
            # For a replaced default branch/tag, a full audit is stricter.
            base = None
    return head, base


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--revision", default="HEAD")
    parser.add_argument("--base", help="Check commits reachable from revision and not base; omit to check all reachable history")
    parser.add_argument("--ci", action="store_true", help="Select submitted push/PR commits from the GitHub event payload")
    args = parser.parse_args()
    if args.ci and (args.base is not None or args.revision != "HEAD"):
        parser.error("--ci cannot be combined with an explicit revision or base")
    try:
        revision, base = ci_range(args.repository) if args.ci else (args.revision, args.base)
        issues = check_identities(args.repository, revision, base)
    except (ValueError, KeyError, TypeError, OSError, subprocess.CalledProcessError) as exc:
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
