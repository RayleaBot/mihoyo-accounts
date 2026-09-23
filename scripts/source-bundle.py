"""Writes .rayleabot/source.zip, the corresponding source the management page
offers for download: this plugin and the RayleaBot SDK it builds against.

Usage: python scripts/source-bundle.py [RayleaBot checkout]
The RayleaBot checkout defaults to ../../RayleaBot.
"""
import pathlib
import sys
import zipfile

plugin = pathlib.Path(__file__).resolve().parents[1]
core = pathlib.Path(sys.argv[1]).resolve() if len(sys.argv) > 1 else plugin.parent.parent / "RayleaBot"
ignored_dirs = {".git", ".rayleabot", "node_modules", "dist", "data", "__pycache__", ".tmp", "logs", ".cache"}
allowed_suffixes = {".go", ".mod", ".sum", ".work", ".json", ".md", ".txt", ".ts", ".js", ".mjs", ".vue", ".css", ".html", ".yaml", ".yml", ".py"}
allowed_names = {"LICENSE", ".gitignore", ".npmrc"}
blocked_names = {"master-key.json", "go.work.sum", ".env", "credentials.json", "cookies.json"}


def add_tree(archive, source, destination):
    for file in sorted(source.rglob("*")):
        relative = file.relative_to(source)
        if any(part in ignored_dirs for part in relative.parts) or not file.is_file() or file.is_symlink():
            continue
        if file.name in blocked_names or file.name.startswith(".env."):
            continue
        if file.suffix.lower() not in allowed_suffixes and file.name not in allowed_names:
            continue
        archive.write(file, (destination / relative).as_posix())


if not (core / "sdk/go/go.mod").is_file():
    raise SystemExit(f"{core} is not a RayleaBot checkout.")
target = plugin / ".rayleabot/source.zip"
target.parent.mkdir(exist_ok=True)
with zipfile.ZipFile(target, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
    add_tree(archive, plugin, pathlib.PurePosixPath("RayleaBotPlugins", plugin.name))
    add_tree(archive, core / "sdk/go", pathlib.PurePosixPath("RayleaBot/sdk/go"))
    add_tree(archive, core / "sdk/vue", pathlib.PurePosixPath("RayleaBot/sdk/vue"))
    archive.write(core / "LICENSE", "RayleaBot/LICENSE")
    archive.writestr("BUILD.md", f"# Corresponding source\n\nOpen RayleaBotPlugins/{plugin.name} and follow README.md. Its go.work uses the included RayleaBot SDK; run scripts/prepare-ui.mjs before installing the UI dependencies. No runtime data or credentials are included.\n")
print(f"{target.relative_to(plugin).as_posix()} written")
