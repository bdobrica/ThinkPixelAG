"""Only the supported managed mapping and OPA configuration contracts."""
import difflib
import json
import re
from urllib.parse import urlsplit
from fastapi import Request
from .policies import exact, proposal
from .security import Failure
from .workflows import number, reference

BASE = "/v1/admin"
ROLES = ("agent-invoker", "registry-admin", "resource-admin", "policy-admin", "revocation-admin")


def mappings(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError()
            result[key] = value
        return result
    try:
        data = json.loads(raw, object_pairs_hook=unique)
        if not isinstance(data, dict) or not 1 <= len(data) <= 256:
            raise ValueError()
        for key, value in data.items():
            if not 1 <= len(key) <= 128 or any(ord(x) < 32 for x in key) or value not in ROLES:
                raise ValueError()
        return data
    except (ValueError, TypeError):
        raise Failure(400, "Use a JSON mapping of unique external role names to the listed internal roles.") from None


def endpoint(value):
    try:
        u = urlsplit(value)
        if not 0 < len(value) <= 2048 or u.scheme not in ("http", "https") or not u.hostname or u.username or u.password or u.path or u.query or u.fragment or any(x.isspace() for x in value):
            raise ValueError()
        _ = u.port
        return value
    except ValueError:
        raise Failure(400, "Use an exact HTTP(S) origin without credentials, path or query.") from None


def install(app, c):
    async def editable(session, path, revision):
        data = await c.api(session, path)
        if data.get("mode") != "api":
            raise Failure(403, "This configuration is file-managed and read-only. Update deployment configuration.")
        if data.get("revision") != revision:
            raise Failure(409, "Configuration changed. Reload and review the current revision.")
        return data

    async def mapping_change(session, v, approval=False):
        exact(v, "expected_revision mappings approval_reference")
        revision = number(v["expected_revision"])
        data = await editable(session, BASE + "/role-mappings", revision)
        proposed = mappings(v["mappings"])
        before = json.dumps(data["mappings"], indent=2, sort_keys=True)
        after = json.dumps(proposed, indent=2, sort_keys=True)
        diff = '\n'.join(difflib.unified_diff(before.splitlines(), after.splitlines(), fromfile='Current mappings', tofile='Proposed mappings', lineterm=''))
        body = {"expected_revision": revision, "mappings": proposed, "approval_reference": "not-required" if approval else reference(v["approval_reference"])}
        return proposal("Request mapping approval" if approval else "Update role mappings", BASE + "/role-mappings" + ("/approvals" if approval else ""), body, "POST" if approval else "PUT", diff=diff)

    async def mapping_approval(session, v):
        return await mapping_change(session, v, approval=True)

    async def opa(session, v):
        exact(v, "expected_revision endpoint token_reference")
        revision = number(v["expected_revision"])
        data = await editable(session, BASE + "/integrations/opa", revision)
        alias = v["token_reference"]
        if alias and not re.fullmatch(r"[a-zA-Z0-9_.:-]{1,128}", alias):
            raise Failure(400, "Use a configured secret alias, never a token or file path.")
        connection = {"endpoint": endpoint(v["endpoint"]), "token_reference": alias}
        diff = '\n'.join(difflib.unified_diff(json.dumps(data['connection'], indent=2, sort_keys=True).splitlines(), json.dumps(connection, indent=2, sort_keys=True).splitlines(), fromfile='Current connection', tofile='Proposed connection', lineterm=''))
        return proposal("Update OPA connection", BASE + "/integrations/opa", {"expected_revision": revision, "connection": connection}, "PUT", diff=diff)

    c.actions.update(mappings=mapping_change, mapping_approval=mapping_approval, opa=opa)

    @app.get("/mappings")
    async def mapping_view(request: Request):
        session = c.session(request)
        data = await c.api(session, BASE + "/role-mappings")
        return c.page(request, "mappings.html", "External role mappings", session, data=data, roles=ROLES,
                      mapping_source=json.dumps(data["mappings"], indent=2, sort_keys=True))

    @app.get("/integrations")
    async def integration_view(request: Request):
        session = c.session(request)
        data = await c.api(session, BASE + "/integrations/opa")
        try:
            status = await c.api(session, BASE + "/integrations/opa/status")
        except Failure as error:
            status = {"state": "unavailable", "explanation": error.message}
        return c.page(request, "integrations.html", "Integration settings", session, data=data, integration_status=status)
