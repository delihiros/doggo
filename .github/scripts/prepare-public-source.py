"""Export committed public source into a new repository with one commit."""

import argparse
import io
import os
import re
import subprocess
import tarfile
from pathlib import Path, PurePosixPath
from urllib.parse import unquote, urlparse


ROOT_DOCUMENTS = {"README.md", "SECURITY.md", "PUBLICATION.md"}
PACKAGE_DOCUMENTS = {
    "editors/vscode/README.md",
    "editors/vscode/CHANGELOG.md",
    "examples/hello-ebitengine/README.md",
}
EXTRA_DOCS = {"docs/public-documents.txt", "docs/release_governance.md"}
PUBLIC_ASSETS = {"docs/images/ebitengine-window.png"}
PRIVATE_DOCUMENT_PREFIXES = ("docs/dev/", "docs/superpowers/", "docs/architecture/")
LINK = re.compile(r"\[[^]]*\]\(([^\s)]+)(?:\s+[^)]*)?\)")
HOME_PATH = re.compile(r"/(?:home|Users)/[A-Za-z0-9_.-]+|[A-Za-z]:[\\/](?:Users|home)[\\/][A-Za-z0-9_.-]+")
GIT_ROUTING_ENV = {
    "GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE",
    "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE",
    "GIT_PREFIX", "GIT_CEILING_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_COUNT",
    "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_TEMPLATE_DIR",
}


def git(repository, *arguments, env=None):
    environment = dict(os.environ if env is None else env)
    for name in list(environment):
        if name in GIT_ROUTING_ENV or name.startswith(("GIT_CONFIG_KEY_", "GIT_CONFIG_VALUE_")):
            del environment[name]
    environment.update({"GIT_CONFIG_GLOBAL": os.devnull, "GIT_CONFIG_NOSYSTEM": "1", "GIT_ATTR_NOSYSTEM": "1", "GIT_NOREPLACE_OBJECTS": "1"})
    return subprocess.check_output(["git", *arguments], cwd=repository, env=environment)


def validate_documents(root):
    for document in root.rglob("*.md"):
        content = document.read_text(encoding="utf-8")
        if HOME_PATH.search(content):
            raise ValueError(f"Local home path in public document: {document.relative_to(root)}")
        for match in LINK.finditer(content):
            target = match.group(1).strip("<>")
            parsed = urlparse(target)
            if parsed.scheme in {"http", "https", "mailto"} or target.startswith("#"):
                continue
            if parsed.scheme or parsed.netloc:
                raise ValueError(f"Unsupported document link: {target}")
            resolved = (document.parent / unquote(parsed.path)).resolve()
            if root != resolved and root not in resolved.parents:
                raise ValueError(f"Document link escapes public source: {target}")
            if not resolved.exists():
                raise ValueError(f"Broken public document link in {document.relative_to(root)}: {target}")


def export_public_source(repository, destination, ref="HEAD"):
    if not ref or ref.startswith("-"):
        raise ValueError("Invalid source ref")
    repository = Path(repository).resolve()
    destination = Path(destination).resolve()
    if repository == destination or repository in destination.parents:
        raise ValueError("Public destination must be outside the source repository")
    if destination.exists():
        raise FileExistsError(f"Destination already exists: {destination}")
    commit = git(repository, "rev-parse", "--verify", f"{ref}^{{commit}}").decode().strip()
    if not re.fullmatch(r"[0-9a-f]{40}|[0-9a-f]{64}", commit):
        raise ValueError("Source ref did not resolve to one commit")
    manifest = git(repository, "show", f"{commit}:docs/public-documents.txt").decode("utf-8")
    documents = {line.strip() for line in manifest.splitlines() if line.strip()}
    for name in documents:
        if name.startswith(PRIVATE_DOCUMENT_PREFIXES):
            raise ValueError(f"Manifest contains a private document: {name}")
        if PurePosixPath(name).is_absolute() or ".." in PurePosixPath(name).parts:
            raise ValueError(f"Invalid public document path: {name}")
    archive = git(repository, "archive", "--format=tar", commit)
    destination.mkdir(parents=True, exist_ok=False)
    exported = set()
    executable = []
    with tarfile.open(fileobj=io.BytesIO(archive)) as source:
        for member in source:
            name = member.name
            path = PurePosixPath(name)
            if path.is_absolute() or ".." in path.parts:
                raise ValueError(f"Invalid source path: {name}")
            if name.startswith((".git/", ".claude/")):
                continue
            if name.startswith("docs/") and name not in documents | EXTRA_DOCS | PUBLIC_ASSETS:
                continue
            if name.endswith(".md") and name not in documents | ROOT_DOCUMENTS | PACKAGE_DOCUMENTS | EXTRA_DOCS:
                continue
            if member.isdir():
                continue
            if not member.isfile():
                raise ValueError(f"Public snapshot requires ordinary files: {name}")
            target = destination.joinpath(*path.parts)
            target.parent.mkdir(parents=True, exist_ok=True)
            with source.extractfile(member) as content:
                target.write_bytes(content.read())
            target.chmod(member.mode & 0o777)
            exported.add(name)
            if member.mode & 0o111:
                executable.append(name)
    if missing := documents - exported:
        raise ValueError(f"Missing public documents: {sorted(missing)}")
    validate_documents(destination)
    git(destination, "init", "-q")
    git(destination, "symbolic-ref", "HEAD", "refs/heads/main")
    git(destination, "config", "user.name", "delihiros")
    git(destination, "config", "user.email", "delihiros@users.noreply.github.com")
    paths = sorted(exported)
    for offset in range(0, len(paths), 100):
        git(destination, "add", "--force", "--", *paths[offset:offset + 100])
    staged = set(filter(None, git(destination, "ls-files", "-z").decode().split("\0")))
    if staged != exported:
        raise ValueError("Public Git index does not match the selected source files")
    for name in executable:
        git(destination, "update-index", "--chmod=+x", name)
    timestamp = git(repository, "show", "-s", "--format=%ct", commit).decode().strip()
    environment = dict(os.environ)
    environment.update({
        "GIT_AUTHOR_NAME": "delihiros",
        "GIT_AUTHOR_EMAIL": "delihiros@users.noreply.github.com",
        "GIT_COMMITTER_NAME": "delihiros",
        "GIT_COMMITTER_EMAIL": "delihiros@users.noreply.github.com",
        "GIT_AUTHOR_DATE": f"@{timestamp} +0000",
        "GIT_COMMITTER_DATE": f"@{timestamp} +0000",
    })
    git(destination, "-c", "commit.gpgsign=false", "-c", f"core.hooksPath={destination / '.git' / 'no-hooks'}", "commit", "-qm", "Initial public source", env=environment)
    return sorted(exported)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("destination", type=Path)
    parser.add_argument("--repository", type=Path, default=Path(__file__).resolve().parents[2])
    parser.add_argument("--ref", default="HEAD")
    arguments = parser.parse_args()
    files = export_public_source(arguments.repository, arguments.destination, arguments.ref)
    print(f"Exported {len(files)} files with a new single-commit history to {arguments.destination.resolve()}")


if __name__ == "__main__":
    main()
