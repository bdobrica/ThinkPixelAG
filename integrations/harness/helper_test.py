import datetime as dt
import hashlib
import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest

loader = importlib.machinery.SourceFileLoader("ag_harness", str(Path(__file__).with_name("thinkpixelag-harness")))
spec = importlib.util.spec_from_loader(loader.name, loader)
h = importlib.util.module_from_spec(spec)
loader.exec_module(h)

ID = "01990000-0000-7000-8000-000000000001"
OTHER = "01990000-0000-7000-8000-000000000002"


def document(run=None):
    now = dt.datetime.now(dt.timezone.utc).replace(microsecond=0)
    scope = {"tenant_id": ID, "principal_id": OTHER}
    if run:
        scope["run_id"] = run
    return {"contract_version": h.CONTRACT, "revision": "sha256:" + "a" * 64,
            "scope": scope, "issued_at": now.isoformat(), "expires_at": (now + dt.timedelta(seconds=30)).isoformat(),
            "governance": "ready", "execution": "unsupported", "markdown": "Trusted fixed guidance",
            "operations": [{"id": name, "method": method, "path": path, "prerequisite": "current_identity_and_policy", "authorization": "checked_per_request"} for name, (method, path) in h.ROUTES.items()]}


def response(doc):
    raw = json.dumps(doc).encode()
    return 200, {"ETag": '"' + hashlib.sha256(raw).hexdigest() + '"', "X-AG-Capability-Revision": doc["revision"], "X-AG-Guidance-Expires": doc["expires_at"]}, raw


class HelperTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.token = self.root / "token"
        self.token.write_text("test-private-token")
        self.token.chmod(0o600)
        self.config = self.root / "config.json"
        self.settings = {"origin": "https://ag.example", "token_file": str(self.token), "state_dir": str(self.root / "state")}
        self.write_config()

    def write_config(self):
        self.config.write_text(json.dumps(self.settings))
        self.config.chmod(0o600)

    def test_cache_revalidates_and_never_falls_back_on_denial(self):
        helper = h.Helper(self.config)
        doc = document()
        helper.request = lambda *a, **kw: response(doc)
        self.assertEqual(helper.guidance(), doc)
        def conditional(*a, **kw):
            self.assertTrue(kw["etag"])
            return 304, response(doc)[1], b""
        helper.request = conditional
        self.assertEqual(helper.guidance(), doc)
        def denied(*a, **kw):
            raise h.GuidanceError("AG request rejected (HTTP 403)")
        helper.request = denied
        with self.assertRaises(h.GuidanceError):
            helper.guidance()

    def test_expired_wrong_scope_version_and_unknown_routes_rejected(self):
        for change in ("expired", "scope", "version", "route"):
            with self.subTest(change=change):
                helper = h.Helper(self.config)
                doc = document(ID)
                if change == "expired":
                    doc["expires_at"] = "2020-01-01T00:00:00Z"
                elif change == "scope":
                    doc["scope"]["run_id"] = OTHER
                elif change == "version":
                    doc["contract_version"] = "unknown"
                else:
                    doc["operations"][0]["path"] = "https://attacker.example/secret"
                helper.request = lambda *a, **kw: response(doc)
                with self.assertRaises(h.GuidanceError):
                    helper.guidance(ID)

    def test_token_and_context_changes_use_different_cache_entries(self):
        helper = h.Helper(self.config)
        helper.request = lambda *a, **kw: response(document())
        helper.guidance()
        helper.request = lambda *a, **kw: response(document(ID))
        helper.guidance(ID)
        self.assertEqual(len(list(helper.state.glob("*.json"))), 2)
        self.token.write_text("another-private-token")
        other = h.Helper(self.config)
        self.assertNotEqual(helper.identity_key, other.identity_key)
        def fresh(*a, **kw):
            self.assertIsNone(kw["etag"])
            return response(document())
        other.request = fresh
        other.guidance()

    def test_https_origin_private_files_and_redirect_boundary(self):
        for origin in ("http://127.0.0.1:8080", "https://user:secret@ag.example", "https://ag.example/path", "https://ag.example?query=1"):
            self.settings["origin"] = origin
            self.write_config()
            with self.assertRaises(h.GuidanceError):
                h.Helper(self.config)
        self.settings["origin"] = "https://ag.example"
        self.write_config()
        self.token.chmod(0o644)
        with self.assertRaises(h.GuidanceError):
            h.Helper(self.config)
        self.assertIsNone(h.NoRedirect().redirect_request(None, None, 302, "", {}, "https://other.example"))

    def test_committed_mutation_retains_result_when_refresh_fails(self):
        helper = h.Helper(self.config)
        calls = 0
        def guidance(run=None):
            nonlocal calls
            calls += 1
            if calls > 1:
                raise h.GuidanceError("refresh unavailable")
            return document()
        helper.guidance = guidance
        helper.request = lambda *a, **kw: (201, {}, json.dumps({"id": ID, "state": "ADMITTED"}).encode())
        result = helper.operate("runs.admit", agent_id=ID, objective="test objective", key="stable-test-request-key")
        self.assertEqual(result["id"], ID)
        self.assertFalse(result["guidance_refreshed"])

    def test_stale_discovery_refresh_is_bounded(self):
        helper = h.Helper(self.config)
        calls = []
        def request(*args, **kwargs):
            calls.append(kwargs)
            if len(calls) == 1:
                raise h.GuidanceError("conflict", status=409)
            return response(document())
        helper.request = request
        helper.guidance()
        self.assertEqual(len(calls), 2)
        self.assertNotIn("etag", calls[1])
        def conflict(*args, **kwargs):
            raise h.GuidanceError("conflict", status=409)
        helper.request = conflict
        with self.assertRaises(h.GuidanceError):
            helper.guidance()

    def test_capability_removal_blocks_mutation(self):
        helper = h.Helper(self.config)
        doc = document(ID)
        doc["operations"] = [op for op in doc["operations"] if op["id"] != "runs.cancel"]
        helper.guidance = lambda *a: doc
        def unexpected(*a, **kw):
            self.fail("removed operation reached transport")
        helper.request = unexpected
        with self.assertRaises(h.GuidanceError):
            helper.operate("runs.cancel", run_id=ID, key="cancel-test-request-key")


if __name__ == "__main__":
    unittest.main()
