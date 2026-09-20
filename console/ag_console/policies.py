"""Policy presentation/workflows over the published administration APIs."""
import difflib
from urllib.parse import urlencode
from fastapi import Request
from .security import Failure, uuid7
from .workflows import digest, field, number, reference

BASE = "/v1/admin"


def exact(values, names):
    if set(values) != set(names.split()):
        raise Failure(400, "Unexpected or missing form fields.")


def proposal(title, path, body, method="POST", **extra):
    return dict(title=title, path=path, body=body, method=method, **extra)


def source(value):
    if not value.strip() or len(value.encode()) > 65536:
        raise Failure(400, "The console editor accepts 1–65,536 UTF-8 bytes. Use the API for larger policies.")
    return value


def install(app, c):
    async def save(session, v):
        exact(v, "source expected_revision draft_id")
        revision = number(v["expected_revision"], 0)
        text = source(v["source"])
        previous = ""
        path = BASE + "/policy-drafts"
        if v["draft_id"]:
            path += "/" + uuid7(v["draft_id"])
            old = await c.api(session, path + "?" + urlencode({"revision": revision}))
            if old["revision"] != revision:
                raise Failure(409, "Draft revision changed. Reload before saving.")
            previous = old["source"]
        elif revision != 0:
            raise Failure()
        diff = '\n'.join(difflib.unified_diff(previous.splitlines(), text.splitlines(), fromfile='Saved revision', tofile='Proposed revision', lineterm=''))
        return proposal("Save draft", path, {"source": text, "expected_revision": revision}, "PUT" if v["draft_id"] else "POST", diff=diff)

    async def validate(session, v):
        exact(v, "draft_id revision")
        return proposal("Validate draft", BASE + "/policy-drafts/" + uuid7(v["draft_id"]) + "/validation", {"revision": number(v["revision"])})

    async def promote(session, v):
        exact(v, "draft_id revision digest artifact_revision")
        return proposal("Promote exact draft", BASE + "/policy-drafts/" + uuid7(v["draft_id"]) + "/promotions",
                        {"revision": number(v["revision"]), "digest": digest(v["digest"]), "artifact_revision": number(v["artifact_revision"])})

    async def activate(session, v):
        exact(v, "digest channel reason_code approval_reference")
        return proposal("Activate policy", BASE + "/policies/" + digest(v["digest"]) + "/activations",
                        {"channel": reference(v["channel"]), "reason_code": reference(v["reason_code"]), "approval_reference": reference(v["approval_reference"])})

    async def rollback(session, v):
        exact(v, "digest expected_policy_epoch lifetime_seconds reason_code")
        return proposal("Request rollback approval", BASE + "/policies/" + digest(v["digest"]) + "/rollback-approvals",
                        {"expected_policy_epoch": number(v["expected_policy_epoch"]), "lifetime_seconds": number(v["lifetime_seconds"], 1, 3600), "reason_code": reference(v["reason_code"])})

    async def decide(session, v):
        exact(v, "approval_id approved")
        if v["approved"] not in ("true", "false"):
            raise Failure()
        return proposal("Decide approval", BASE + "/approvals/" + uuid7(v["approval_id"]) + "/decisions", {"approved": v["approved"] == "true"})

    c.actions.update(save=save, validate=validate, promote=promote, activate=activate, rollback=rollback, decide=decide)

    @app.get("/policies")
    @app.get("/drafts")
    @app.get("/history")
    async def listing(request: Request):
        session = c.session(request)
        kind = request.url.path[1:]
        endpoints = {"policies": "policies", "drafts": "policy-drafts", "history": "policy-activations"}
        cursor = request.query_params.get("cursor", "")
        if len(cursor) > 512 or set(request.query_params) - {"cursor"} or len(request.query_params.getlist("cursor")) > 1:
            raise Failure()
        data = await c.api(session, BASE + "/" + endpoints[kind] + "?" + urlencode({"limit": 50, "cursor": cursor}))
        return c.page(request, "policy_list.html", {"policies": "Policy artifacts", "drafts": "Policy drafts", "history": "Activation history"}[kind], session,
                      data=data, kind=kind, next_url="/" + kind + "?" + urlencode({"cursor": data.get("next_cursor", "")}))

    @app.get("/drafts/new")
    async def new(request: Request):
        session = c.session(request)
        text = ""
        if request.query_params.get("from"):
            d = digest(request.query_params["from"])
            text = (await c.api(session, BASE + "/policies/" + d + "/source"))["source"]
        return c.page(request, "draft.html", "New policy draft", session, draft={"id": "", "revision": 0, "source": text}, diff="")

    @app.get("/drafts/{identifier}")
    async def draft(request: Request, identifier: str):
        session = c.session(request)
        revision = number(request.query_params.get("revision", "0"), 0)
        path = BASE + "/policy-drafts/" + uuid7(identifier)
        data = await c.api(session, path + "?" + urlencode({"revision": revision}))
        previous = ""
        if data["revision"] > 1:
            previous = (await c.api(session, path + "?" + urlencode({"revision": data["revision"]-1})))["source"]
        diff = '\n'.join(difflib.unified_diff(previous.splitlines(), data["source"].splitlines(), fromfile='Previous revision', tofile='This revision', lineterm=''))
        return c.page(request, "draft.html", "Edit policy draft", session, draft=data, diff=diff)

    @app.get("/policies/{policy_digest}")
    async def policy(request: Request, policy_digest: str):
        session = c.session(request)
        d = digest(policy_digest)
        data = await c.api(session, BASE + "/policies/" + d + "/source")
        current = await c.api(session, BASE + "/policy-activations/current")
        return c.page(request, "policy.html", "Signed policy artifact", session, data=data, current=current)

    @app.get("/policies/{policy_digest}/{operation}")
    async def policy_form(request: Request, policy_digest: str, operation: str):
        session = c.session(request)
        d = digest(policy_digest)
        await c.api(session, BASE + "/policies/" + d)
        current = await c.api(session, BASE + "/policy-activations/current")
        fields = [field("digest", "Exact artifact digest", d, readonly=True)]
        if operation == "activate":
            fields += [field("channel", "Channel", current["channel"], readonly=True), field("reason_code", "Reason code", "policy.activate"), field("approval_reference", "Approval ID, or not-required for a first activation", "not-required")]
            note = "Activation changes governance. For a rollback, first request independent approval and supply its ID. AG checks the exact approved action and current epoch."
        elif operation == "rollback":
            fields += [field("expected_policy_epoch", "Current policy epoch", current["policy_epoch"], readonly=True), field("lifetime_seconds", "Approval lifetime (seconds)", 900, "number"), field("reason_code", "Reason code", "policy.rollback")]
            note = "Request an approval for this exact rollback. A different operator must decide it before activation."
        else:
            raise Failure(404)
        return c.page(request, "form.html", "Activate policy" if operation == "activate" else "Request rollback approval", session, fields=fields, action=operation, note=note)

    @app.get("/approvals")
    async def approval_lookup(request: Request):
        session = c.session(request)
        identifier = request.query_params.get("id")
        if identifier:
            from fastapi.responses import RedirectResponse
            return RedirectResponse("/approvals/" + uuid7(identifier), status_code=303)
        return c.page(request, "approval_lookup.html", "Find approval", session)

    @app.get("/approvals/{identifier}")
    async def approval(request: Request, identifier: str):
        session = c.session(request)
        data = await c.api(session, BASE + "/approvals/" + uuid7(identifier))
        return c.page(request, "approval.html", "Independent approval", session, data=data)
