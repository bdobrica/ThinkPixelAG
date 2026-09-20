"""Optional console factory. All governance reads and writes use AG HTTP APIs."""
import asyncio
from pathlib import Path
import secrets
import time
from urllib.parse import parse_qs, urlencode, urlsplit

from fastapi import FastAPI, Request
from fastapi.responses import HTMLResponse, RedirectResponse, PlainTextResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates

from .security import Failure, OIDC, Sessions, Settings, Transport, uuid7

COOKIE = "__Host-ag-console"
LOGIN_COOKIE = "__Host-ag-login"
ROOT = Path(__file__).parent


class Boundary:
    def __init__(self, app, settings):
        self.app, self.settings = app, settings

    async def __call__(self, scope, receive, send):
        if scope["type"] != "http":
            return await self.app(scope, receive, send)
        headers = {}
        for key, value in scope["headers"]:
            headers.setdefault(key, []).append(value)
        async def secure_send(message):
            if message["type"] == "http.response.start":
                message.setdefault("headers", []).extend([
                    (b"cache-control", b"no-store"), (b"referrer-policy", b"strict-origin"),
                    (b"x-content-type-options", b"nosniff"), (b"x-frame-options", b"DENY"),
                    (b"strict-transport-security", b"max-age=31536000"),
                    (b"content-security-policy", b"default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")])
            await send(message)
        try:
            if headers.get(b"host") != [urlsplit(self.settings.public_origin).netloc.encode()]:
                raise Failure(400, "Unexpected console host.")
            if len(headers.get(b"cookie", [])) > 1 or len(scope.get("query_string", b"")) > 4096:
                raise Failure()
            if scope["method"] == "POST" and headers.get(b"origin") != [self.settings.public_origin.encode()]:
                raise Failure(403, "Same-origin form submission required.")
            raw = bytearray()
            async with asyncio.timeout(10):
                while True:
                    message = await receive()
                    if message["type"] == "http.disconnect":
                        return
                    raw.extend(message.get("body", b""))
                    if len(raw) > 1024 * 1024:
                        raise Failure(413, "Console form exceeds one MiB.")
                    if not message.get("more_body"):
                        break
            delivered = False
            async def bounded_receive():
                nonlocal delivered
                if delivered:
                    return await receive()
                delivered = True
                return {"type": "http.request", "body": bytes(raw), "more_body": False}
            await self.app(scope, bounded_receive, secure_send)
        except Failure as error:
            await PlainTextResponse(error.message, status_code=error.status)(scope, receive, secure_send)
        except TimeoutError:
            await PlainTextResponse("Request timeout.", status_code=408)(scope, receive, secure_send)


class Console:
    def __init__(self, settings, transport):
        self.s, self.transport = settings, transport
        self.sessions = Sessions()
        self.oidc = OIDC(settings, transport)
        self.templates = Jinja2Templates(directory=str(ROOT / "templates"))

    def session(self, request):
        self.sessions.prune()
        value = self.sessions.active.get(request.cookies.get(COOKIE))
        if not value:
            raise Failure(401, "Sign in to continue.")
        return value

    async def form(self, request):
        session = self.session(request)
        if request.headers.get("content-type", "").split(";")[0] != "application/x-www-form-urlencoded":
            raise Failure(415, "URL-encoded form required.")
        try:
            values = parse_qs((await request.body()).decode(), keep_blank_values=True, max_num_fields=24, strict_parsing=True)
            if any(len(v) != 1 for v in values.values()):
                raise ValueError()
            values = {k: v[0] for k, v in values.items()}
        except (ValueError, UnicodeError):
            raise Failure() from None
        if not secrets.compare_digest(values.pop("csrf", ""), session.csrf):
            raise Failure(403, "Form expired or CSRF check failed. Reload the page.")
        return session, values

    async def api(self, session, path, method="GET", body=None, key=None):
        # Paths are constructed only by fixed handlers, never accepted as URLs.
        return await self.transport.json(self.s.ag_origin + path, method, token=session.token, body=body, key=key)

    def page(self, request, name, title, session=None, status=200, **values):
        return self.templates.TemplateResponse(request=request, name=name, context={"title": title, "session": session, **values}, status_code=status)


def create_app(settings=None, transport=None):
    settings = settings or Settings.environment()
    c = Console(settings, transport or Transport(settings))
    app = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)
    app.state.console = c
    app.add_middleware(Boundary, settings=settings)
    app.mount("/static", StaticFiles(directory=str(ROOT / "static")), name="static")

    @app.exception_handler(Failure)
    async def failure(request, error):
        return c.page(request, "error.html", "Request not completed", status=error.status, message=error.message)

    @app.exception_handler(Exception)
    async def unexpected(request, error):
        # Never reflect upstream bodies, access tokens, URLs or exception details.
        return c.page(request, "error.html", "Request not completed", status=500, message="Console could not complete the request.")

    @app.get("/healthz")
    async def health():
        return PlainTextResponse("ok")

    @app.get("/login")
    async def login(request: Request):
        if request.query_params or request.headers.get("sec-fetch-site") == "cross-site":
            raise Failure()
        handle, pending = c.sessions.login()
        try:
            destination = await c.oidc.start(pending)
        except Failure:
            c.sessions.logins.pop(handle, None)
            raise
        response = RedirectResponse(destination, status_code=303)
        response.set_cookie(LOGIN_COOKIE, handle, secure=True, httponly=True, samesite="lax", max_age=300, path="/")
        return response

    @app.get("/callback")
    async def callback(request: Request):
        c.sessions.prune()
        pending = c.sessions.logins.pop(request.cookies.get(LOGIN_COOKIE), None)
        params = request.query_params
        if not pending or set(params) - {"code", "state", "iss"} or any(len(params.getlist(k)) != 1 for k in params) or not secrets.compare_digest(params.get("state", ""), pending["state"]) or not 0 < len(params.get("code", "")) <= 2048:
            raise Failure(401, "Login expired or callback rejected. Sign in again.")
        if "iss" in params and params["iss"] != settings.issuer:
            raise Failure(401, "Identity provider mismatch.")
        session = await c.oidc.finish(pending, params["code"])
        c.sessions.active.pop(request.cookies.get(COOKIE), None)
        handle = c.sessions.add(session)
        response = RedirectResponse("/", status_code=303)
        response.delete_cookie(LOGIN_COOKIE, secure=True, httponly=True, samesite="lax", path="/")
        response.set_cookie(COOKIE, handle, secure=True, httponly=True, samesite="lax", max_age=max(1, int(session.expires-time.time())), path="/")
        return response

    @app.post("/logout")
    async def logout(request: Request):
        await c.form(request)
        c.sessions.active.pop(request.cookies.get(COOKIE), None)
        response = RedirectResponse("/", status_code=303)
        response.delete_cookie(COOKIE, secure=True, httponly=True, samesite="lax", path="/")
        return response

    @app.get("/")
    async def home(request: Request):
        try:
            session = c.session(request)
        except Failure:
            return c.page(request, "welcome.html", "Govern your agents")
        cards = []
        for title, path in (("Active policy", "/v1/admin/policy-activations/current"), ("OPA configuration", "/v1/admin/integrations/opa"), ("OPA readiness", "/v1/admin/integrations/opa/status")):
            try:
                data = await c.api(session, path)
                cards.append({"title": title, "data": data})
            except Failure as error:
                cards.append({"title": title, "error": error.message})
        return c.page(request, "home.html", "Governance overview", session, cards=cards)

    @app.get("/agents")
    @app.get("/runs")
    async def listing(request: Request):
        session = c.session(request)
        kind = request.url.path[1:]
        cursor = request.query_params.get("cursor", "")
        if set(request.query_params) - {"cursor"} or len(request.query_params.getlist("cursor")) > 1 or len(cursor) > 512:
            raise Failure()
        data = await c.api(session, "/v1/" + kind + "?" + urlencode({"limit": 50, "cursor": cursor}))
        return c.page(request, "list.html", kind.capitalize(), session, data=data, kind=kind,
                      next_url="/" + kind + "?" + urlencode({"cursor": data.get("next_cursor", "")}))

    @app.get("/agents/{identifier}")
    @app.get("/runs/{identifier}")
    async def detail(request: Request, identifier: str):
        session = c.session(request)
        kind = request.url.path.split("/")[1]
        data = await c.api(session, "/v1/" + kind + "/" + uuid7(identifier))
        return c.page(request, "detail.html", "Agent" if kind == "agents" else "Run", session, data=data)

    from . import workflows, policies, configuration
    workflows.install(app, c)
    policies.install(app, c)
    configuration.install(app, c)
    return app
