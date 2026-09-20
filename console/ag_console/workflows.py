"""Transient reviewed requests. AG retains all authority and replay decisions."""
import re
import secrets
import time
from fastapi import Request
from fastapi.responses import RedirectResponse
from .security import Failure


def number(value, minimum=1, maximum=2**53):
    if not isinstance(value, str) or not re.fullmatch(r"[0-9]{1,16}", value):
        raise Failure(400, "A bounded integer revision is required.")
    result = int(value)
    if not minimum <= result <= maximum:
        raise Failure(400, "Number outside the accepted range.")
    return result


def digest(value):
    if not isinstance(value, str) or not re.fullmatch(r"sha256:[a-f0-9]{64}", value):
        raise Failure(400, "An exact SHA-256 digest is required.")
    return value


def reference(value):
    if not re.fullmatch(r"[a-zA-Z0-9_.:-]{1,128}", value):
        raise Failure(400, "Invalid approval reference.")
    return value


def field(name, label, value="", kind="text", **extra):
    return dict(name=name, label=label, value=value, kind=kind, **extra)


def install(app, c):
    c.actions = {}

    def prune():
        for session in c.sessions.active.values():
            for key, intent in list(session.intents.items()):
                if intent["expires"] <= time.time():
                    del session.intents[key]

    def current(session, identifier):
        prune()
        intent = session.intents.get(identifier)
        if not intent:
            raise Failure(404, "Review expired or belongs to another session. Reload current AG state.")
        return intent

    @app.post("/prepare/{action}")
    async def prepare(request: Request, action: str):
        session, values = await c.form(request)
        if action not in c.actions:
            raise Failure(404)
        prune()
        if len(session.intents) >= 10 or sum(len(s.intents) for s in c.sessions.active.values()) >= 100:
            raise Failure(429, "Too many pending reviews. Wait for old reviews to expire.")
        intent = await c.actions[action](session, values)
        # Preparing may await AG; concurrent requests can fill the remaining slots.
        if len(session.intents) >= 10 or sum(len(s.intents) for s in c.sessions.active.values()) >= 100:
            raise Failure(429, "Too many pending reviews. Try later.")
        intent.update(action=action, key="console-" + secrets.token_hex(24), expires=time.time()+600, busy=False, result=None)
        identifier = secrets.token_urlsafe(24)
        session.intents[identifier] = intent
        return RedirectResponse("/review/" + identifier, status_code=303)

    @app.get("/review/{identifier}")
    async def review(request: Request, identifier: str):
        session = c.session(request)
        intent = current(session, identifier)
        return c.page(request, "review.html", "Review " + intent["title"].lower(), session, intent=intent, identifier=identifier)

    @app.post("/review/{identifier}")
    async def apply(request: Request, identifier: str):
        session, values = await c.form(request)
        if values:
            raise Failure()
        intent = current(session, identifier)
        if intent["busy"]:
            raise Failure(409, "This request is in progress. Reload its review page.")
        if intent["result"] is None:
            intent["busy"] = True
            try:
                # No mutation retries or reconstructed authority in the BFF.
                result = await c.api(session, intent["path"], intent["method"], intent["body"], intent["key"])
                intent["result"] = result
            finally:
                intent["busy"] = False
        return RedirectResponse("/review/" + identifier, status_code=303)
