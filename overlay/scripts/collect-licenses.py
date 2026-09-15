#!/usr/bin/env python3
"""Collect license texts for files bundled into the Stratux Debian package."""

import argparse
import json
import pathlib
import re
import shutil
import subprocess


LICENSE_NAMES = ("LICENSE*", "LICENCE*", "COPYING*", "NOTICE*")


def safe_name(value: str) -> str:
    return re.sub(r"[^A-Za-z0-9._+-]+", "_", value).strip("_")


def copy_license_files(source: pathlib.Path, destination: pathlib.Path) -> list[str]:
    copied = []
    if not source.is_dir():
        return copied
    for pattern in LICENSE_NAMES:
        for path in sorted(source.glob(pattern)):
            if not path.is_file():
                continue
            target = destination / path.name
            destination.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
            copied.append(str(target))
    return copied


def decode_json_stream(value: str) -> list[dict]:
    decoder = json.JSONDecoder()
    position = 0
    records = []
    while position < len(value):
        while position < len(value) and value[position].isspace():
            position += 1
        if position >= len(value):
            break
        record, position = decoder.raw_decode(value, position)
        records.append(record)
    return records


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    source = pathlib.Path(args.source).resolve()
    output = pathlib.Path(args.output).resolve()
    shutil.rmtree(output, ignore_errors=True)
    output.mkdir(parents=True)

    index = ["Stratux NX bundled license files", ""]

    direct_files = {
        "stratux-upstream": source / "LICENSE",
        "stratux-nx": source / "debian/stratux-nx-license",
        "stratux-nx-notices": source / "debian/STRATUX-NX-NOTICES.md",
    }
    for name, path in direct_files.items():
        if not path.is_file():
            raise SystemExit(f"Required license file is missing: {path}")
        target = output / f"{name}-{path.name}"
        shutil.copyfile(path, target)
        index.append(f"{name}: {target.name}")

    for relative in ("dump978", "dump1090", "rtl-ais", "ogn/ogn-tracker"):
        destination = output / safe_name(relative)
        copied = copy_license_files(source / relative, destination)
        if copied:
            index.append(f"{relative}: {destination.name}/")

    result = subprocess.run(
        ["go", "list", "-m", "-json", "all"],
        cwd=source,
        check=True,
        capture_output=True,
        text=True,
    )
    modules = decode_json_stream(result.stdout)
    module_count = 0
    for module in modules:
        module_path = str(module.get("Path", ""))
        module_dir = pathlib.Path(str(module.get("Dir", "")))
        if not module_path or not module_dir.is_dir() or module.get("Main"):
            continue
        destination = output / "go-modules" / safe_name(module_path)
        copied = copy_license_files(module_dir, destination)
        if copied:
            module_count += 1
            index.append(f"{module_path}: go-modules/{destination.name}/")

    if module_count == 0:
        raise SystemExit("No Go module license files were collected")

    (output / "INDEX.txt").write_text("\n".join(index) + "\n")
    print(f"Collected license files for {module_count} Go modules.")


if __name__ == "__main__":
    main()
