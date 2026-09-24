"""Copies the fonts and images the login card (templates/uid-list) draws from
the pinned reference snapshots into assets/<source>/, at the upstream commits
the game plugins bundle. The plugin build ships the top-level assets/
directory, which is not tracked; assets/<source>.json records the commit.

Usage: python scripts/bundle-assets.py --references <参考项目/2026-09-15>

Run it before building a package.
"""
import argparse
import json
import pathlib
import shutil

# Source id: the reference snapshot, its pinned commit and the files the page
# uses. internal/app/login_card.go names the same paths.
BUNDLES = {
    "miao-plugin": ("miao-plugin", "7f6f1c84c89102bc6b1c58c8e1b06c61f4642161", [
        "resources/common/font/tttgbnumber.woff",
        "resources/common/font/HYWH-65W.woff",
        "resources/common/bg/bg-hydro.webp",
        "resources/common/item/face.webp",
        "resources/meta-gs/character/common/imgs/banner.webp",
        "resources/meta-sr/character/common/imgs/banner.webp",
    ]),
    "yunzai-genshin": ("Yunzai-genshin", "4a2e1fb8f094b2a8f039ce0768773701718b60b9", [
        "resources/ZZZero/img/other/banner.png",
    ]),
}

parser = argparse.ArgumentParser()
parser.add_argument("--references", type=pathlib.Path, required=True)
args = parser.parse_args()
refs = args.references.resolve()
root = pathlib.Path(__file__).resolve().parents[1]

for source_id, (snapshot, commit, names) in BUNDLES.items():
    pinned = json.loads((refs / f"{snapshot}.source.json").read_text(encoding="utf-8"))
    if pinned["commit"] != commit:
        raise SystemExit(f"{snapshot} is at {pinned['commit']}, not the pinned {commit}")
    target = root / "assets" / source_id
    shutil.rmtree(target, ignore_errors=True)
    size = 0
    for name in names:
        file = target / name
        file.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(refs / snapshot / name, file)
        size += file.stat().st_size
    record = {"commit": commit, "archive": pinned["archive_url"], "files": len(names), "bytes": size}
    (root / "assets" / f"{source_id}.json").write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    print(f"assets/{source_id}: {len(names)} files, {size / 1e6:.1f} MB at {commit[:7]}")
