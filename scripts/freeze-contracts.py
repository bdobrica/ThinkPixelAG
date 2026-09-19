#!/usr/bin/env python3
"""Explicitly regenerate the reviewed RC contract fingerprint manifest."""
import hashlib
import json
from pathlib import Path

root = Path(__file__).resolve().parent.parent
paths = [root / "api/openapi/thinkpixelag.yaml", root / "docs/contracts/policy-decision.md", root / "internal/policy/contract.go", *sorted((root / "api/schemas").glob("*.json"))]
manifest = {
    "baseline_revision": "72715e115a277cdd4178708c1663bdff49b2cbc6",
    "openapi_version": "0.1.0-rc.1",
    "policy_version": "thinkpixelag.authorization/v1alpha1",
    "normalization": "CRLF to LF; no other normalization",
    "files": {str(p.relative_to(root)): hashlib.sha256(p.read_bytes().replace(b"\r\n", b"\n")).hexdigest() for p in sorted(paths)},
}
(root / "api/contract-freeze.json").write_text(json.dumps(manifest, indent=2) + "\n")
