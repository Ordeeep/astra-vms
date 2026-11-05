"""Create a review copy from an explicit file list; never publish or copy Git history."""

import argparse
import hashlib
import json
import re
from pathlib import Path, PurePosixPath


ROOT = Path(__file__).resolve().parents[1]
LIST = ROOT / "tools" / "public-source-files.txt"
FORBIDDEN_PARTS = {
    ".git", ".scratch", ".tools", "tmp", "output", "node_modules", ".next",
    ".playwright-cli", "bin", "dist", "data", "pgdata", "archive", ".generated",
    "test-results", "playwright-report", "__pycache__",
}
TEXT_SUFFIXES = {".md", ".txt", ".go", ".mod", ".sum", ".sql", ".ts", ".tsx",
                 ".css", ".mjs", ".cjs", ".json", ".ps1", ".py", ".toml", ".svg"}
ASSETS = {"frontend/src/app/apple-icon.png", "frontend/src/app/favicon.ico"}
DOTFILES = {".gitignore", ".gitleaks.toml", "LICENSE"}


def selected_files():
    entries = [line.strip() for line in LIST.read_text(encoding="utf-8-sig").splitlines()
               if line.strip() and not line.startswith("#")]
    if len(entries) != len(set(entries)):
        raise ValueError("Duplicate entries in source list")
    result = []
    for entry in entries:
        path = PurePosixPath(entry)
        if path.is_absolute() or "\\" in entry or ":" in entry or ".." in path.parts:
            raise ValueError(f"Invalid relative path: {entry}")
        if any(part.lower() in FORBIDDEN_PARTS for part in path.parts):
            raise ValueError(f"Private/generated path in list: {entry}")
        source = ROOT.joinpath(*path.parts)
        for component in [source, *source.parents]:
            if component == ROOT:
                break
            if component.is_symlink() or getattr(component, "is_junction", lambda: False)():
                raise ValueError(f"Link/reparse point in source path: {entry}")
        if not source.resolve().is_relative_to(ROOT.resolve()) or not source.is_file():
            raise ValueError(f"Missing or external source: {entry}")
        if entry not in ASSETS and source.name not in DOTFILES and source.suffix not in TEXT_SUFFIXES:
            raise ValueError(f"Unreviewed file type: {entry}")
        raw = source.read_bytes()
        if entry not in ASSETS:
            content = raw.decode("utf-8-sig")
            if "\x00" in content:
                raise ValueError(f"Binary content in text file: {entry}")
            if re.search(r"-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----", content):
                raise ValueError(f"Private key in source: {entry}")
        result.append((entry, raw))
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--destination", required=True, type=Path,
                        help="New directory for a local review copy (must not exist)")
    args = parser.parse_args()
    files = selected_files()  # Validate everything before creating the destination.
    destination = args.destination.absolute()
    # Refuse a destination behind a directory junction/symlink as well.
    for component in [destination, *destination.parents]:
        if component.is_symlink() or getattr(component, "is_junction", lambda: False)():
            raise ValueError("Destination must not traverse a link/reparse point")
    if destination.exists():
        raise ValueError("Destination already exists; choose a new directory")
    destination.mkdir(parents=True)
    manifest = []
    for entry, raw in files:
        target = destination.joinpath(*PurePosixPath(entry).parts)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(raw)
        manifest.append({"path": entry, "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()})
    (destination / "source-manifest.json").write_text(json.dumps({
        "purpose": "local review; publication and licensing approval pending",
        "files": manifest,
    }, indent=2) + "\n", encoding="utf-8")
    print(f"Prepared {len(files)} files for local review: {destination}")
    print("No Git history copied. Resolve docs/publication.md before publishing.")


if __name__ == "__main__":
    main()
