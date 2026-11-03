#!/usr/bin/env python3
"""Create an SPDX 2.3 inventory for Tell's native release artifacts."""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
from datetime import datetime, timezone
from pathlib import Path


def module_records(go: str, native_dir: Path) -> list[dict[str, str]]:
    raw = subprocess.check_output([go, "list", "-m", "-json", "all"], cwd=native_dir, text=True)
    decoder = json.JSONDecoder()
    records = []
    offset = 0
    while offset < len(raw):
        while offset < len(raw) and raw[offset].isspace():
            offset += 1
        if offset >= len(raw):
            break
        item, offset = decoder.raw_decode(raw, offset)
        if item.get("Main"):
            continue
        path = item.get("Path", "")
        version = item.get("Version", "")
        if path and version:
            records.append({"path": path, "version": version})
    return records


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--directory", required=True, type=Path)
    parser.add_argument("--native-dir", required=True, type=Path)
    parser.add_argument("--go", default="go")
    args = parser.parse_args()

    root = args.directory.resolve()
    binaries = sorted(path for path in root.glob("tell-*") if path.is_file())
    if len(binaries) != 6:
        raise SystemExit(f"expected six Tell platform binaries in {root}, found {len(binaries)}")

    go_version = subprocess.check_output([args.go, "version"], cwd=args.native_dir, text=True).strip()
    packages = [
        {
            "name": "Tell",
            "SPDXID": "SPDXRef-Tell",
            "versionInfo": args.version,
            "downloadLocation": "https://github.com/KernelGhost-gif/Tell-Code",
            "filesAnalyzed": False,
            "licenseConcluded": "MIT",
            "licenseDeclared": "MIT",
            "copyrightText": "NOASSERTION",
            "primaryPackagePurpose": "APPLICATION",
        },
        {
            "name": "Go standard library",
            "SPDXID": "SPDXRef-Go-Stdlib",
            "versionInfo": go_version.removeprefix("go version ").split()[0],
            "downloadLocation": "https://go.dev/dl/",
            "filesAnalyzed": False,
            "licenseConcluded": "BSD-3-Clause",
            "licenseDeclared": "BSD-3-Clause",
            "copyrightText": "Copyright The Go Authors",
        },
    ]
    relationships = [
        {"spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": "SPDXRef-Tell"},
        {"spdxElementId": "SPDXRef-Tell", "relationshipType": "DEPENDS_ON", "relatedSpdxElement": "SPDXRef-Go-Stdlib"},
    ]
    for index, module in enumerate(module_records(args.go, args.native_dir), start=1):
        package_id = f"SPDXRef-Go-Module-{index}"
        packages.append({
            "name": module["path"],
            "SPDXID": package_id,
            "versionInfo": module["version"],
            "downloadLocation": f"https://{module['path']}" if "." in module["path"].split("/")[0] else "NOASSERTION",
            "filesAnalyzed": False,
            "licenseConcluded": "NOASSERTION",
            "licenseDeclared": "NOASSERTION",
            "copyrightText": "NOASSERTION",
            "externalRefs": [{
                "referenceCategory": "PACKAGE-MANAGER",
                "referenceType": "purl",
                "referenceLocator": f"pkg:golang/{module['path']}@{module['version']}",
            }],
        })
        relationships.append({
            "spdxElementId": "SPDXRef-Tell",
            "relationshipType": "DEPENDS_ON",
            "relatedSpdxElement": package_id,
        })

    files = []
    for binary in binaries:
        checksum = hashlib.sha256(binary.read_bytes()).hexdigest()
        file_id = "SPDXRef-File-" + binary.name.replace(".", "-")
        files.append({
            "fileName": binary.name,
            "SPDXID": file_id,
            "checksums": [{"algorithm": "SHA256", "checksumValue": checksum}],
            "licenseConcluded": "NOASSERTION",
            "copyrightText": "NOASSERTION",
            "fileTypes": ["BINARY"],
        })
        relationships.append({
            "spdxElementId": "SPDXRef-Tell",
            "relationshipType": "CONTAINS",
            "relatedSpdxElement": file_id,
        })

    document = {
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
        "SPDXID": "SPDXRef-DOCUMENT",
        "name": f"Tell {args.version} native release",
        "documentNamespace": f"https://github.com/KernelGhost-gif/Tell-Code/spdx/{args.version}",
        "creationInfo": {
            "creators": ["Tool: scripts/generate-sbom.py", "Organization: Tell Language contributors"],
            "created": datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
        },
        "packages": packages,
        "files": files,
        "relationships": relationships,
    }
    (root / "SBOM.spdx.json").write_text(json.dumps(document, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
