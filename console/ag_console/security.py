"""Bounded HTTPS/OIDC transport and transient, opaque browser sessions."""
import asyncio
import base64
from dataclasses import dataclass, field
import hashlib
import json
import os
import secrets
import ssl
import time
from urllib.parse import urlencode, urlsplit
import uuid

import httpx
import jwt


class Failure(Exception):
    def __init__(self, status=400, message="Invalid request."):
        super().__init__(message)
        self.status, self.message = status, message


def origin(value):
    try:
        u = urlsplit(value)
        if u.scheme != "https" or not u.hostname or u.username or u.password or u.query or u.fragment or u.path or any(c.isspace() for c in value):
            raise ValueError()
        _ = u.port
        return value
    except ValueError:
        raise ValueError("An exact HTTPS origin is required") from None


@dataclass(frozen=True)
class Settings:
    public_origin: str
    ag_origin: str
    issuer: str
    client_id: str
    audience: str
    ca_file: str | None = None
    tenant_claim: str = "tenant_id"

    def __post_init__(self):
        origin(self.public_origin)
        origin(self.ag_origin)
        u = urlsplit(self.issuer)
        origin(f"{u.scheme}://{u.netloc}")
        if u.query or u.fragment or u.path.endswith("/") or not self.client_id or not self.audience:
            raise ValueError("Invalid issuer/client/audience configuration")

    @classmethod
    def environment(cls):
        return cls(*(os.environ["AG_CONSOLE_" + k] for k in ("PUBLIC_ORIGIN", "AG_ORIGIN", "ISSUER", "CLIENT_ID", "AUDIENCE")),
                   ca_file=os.environ.get("AG_CONSOLE_CA_FILE"), tenant_claim=os.environ.get("AG_CONSOLE_TENANT_CLAIM", "tenant_id"))


class Transport:
    def __init__(self, settings, adapter=None):
        self.context = ssl.create_default_context(cafile=settings.ca_file)
        self.adapter = adapter

    async def json(self, url, method="GET", *, token=None, body=None, form=None, key=None):
        headers = {"Accept": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        if key:
            headers["Idempotency-Key"] = key
        try:
            async with asyncio.timeout(12):
                # No persistent cookie jar, redirects, proxies or caller-supplied URLs.
                async with httpx.AsyncClient(verify=self.context, transport=self.adapter, trust_env=False,
                                             follow_redirects=False, timeout=8) as client:
                    async with client.stream(method, url, headers=headers, json=body, data=form) as r:
                        if r.status_code not in (200, 201):
                            messages = {400: "AG rejected the input. Check the source and fields.", 401: "Authentication expired. Sign in again.",
                                        403: "AG denied this operation or requires independent approval.", 404: "Not found or unavailable in this deployment.",
                                        409: "Revision or approval conflict. Reload and review the current state.", 422: "Validation failed. Check the policy source."}
                            raise Failure(r.status_code if r.status_code in messages else 503, messages.get(r.status_code, "Upstream service unavailable. No automatic write retry was made."))
                        if r.headers.get("content-type", "").split(";")[0] != "application/json":
                            raise Failure(502, "Invalid upstream response.")
                        raw = bytearray()
                        async for chunk in r.aiter_bytes():
                            raw.extend(chunk)
                            if len(raw) > 2 * 1024 * 1024:
                                raise Failure(502, "Upstream response exceeds console bounds.")
                        value = json.loads(raw)
                        if not isinstance(value, dict):
                            raise ValueError()
                        return value
        except (httpx.HTTPError, TimeoutError, ValueError, OSError):
            raise Failure(503, "Upstream service unavailable or invalid response.") from None


@dataclass
class Session:
    token: str
    subject: str
    tenant: str
    expires: float
    csrf: str = field(default_factory=lambda: secrets.token_urlsafe(32))
    intents: dict = field(default_factory=dict)


class Sessions:
    def __init__(self):
        self.active, self.logins = {}, {}

    def prune(self):
        now = time.time()
        for store in (self.active, self.logins):
            for key, value in list(store.items()):
                expiry = value.expires if isinstance(value, Session) else value["expires"]
                if expiry <= now:
                    del store[key]

    def login(self):
        self.prune()
        if len(self.logins) >= 1000:
            raise Failure(503, "Login capacity reached. Try later.")
        handle = secrets.token_urlsafe(32)
        pending = {"state": secrets.token_urlsafe(32), "nonce": secrets.token_urlsafe(32),
                   "verifier": secrets.token_urlsafe(48), "expires": time.time() + 300}
        self.logins[handle] = pending
        return handle, pending

    def add(self, session):
        self.prune()
        if len(self.active) >= 1000:
            raise Failure(503, "Session capacity reached. Try later.")
        handle = secrets.token_urlsafe(32)
        self.active[handle] = session
        return handle


def uuid7(value):
    try:
        parsed = uuid.UUID(value)
        if parsed.version != 7 or str(parsed) != value:
            raise ValueError()
        return value
    except (TypeError, ValueError, AttributeError):
        raise Failure(400, "A canonical UUIDv7 is required.") from None


class OIDC:
    def __init__(self, settings, transport):
        self.s, self.transport = settings, transport

    async def metadata(self):
        m = await self.transport.json(self.s.issuer + "/.well-known/openid-configuration")
        if m.get("issuer") != self.s.issuer:
            raise Failure(503, "Identity provider configuration mismatch.")
        authority = urlsplit(self.s.issuer)
        for field in ("authorization_endpoint", "token_endpoint", "jwks_uri"):
            u = urlsplit(m.get(field, ""))
            if (u.scheme, u.netloc) != (authority.scheme, authority.netloc) or u.username or u.password or u.query or u.fragment:
                raise Failure(503, "Identity provider endpoint rejected.")
        if "S256" not in m.get("code_challenge_methods_supported", []):
            raise Failure(503, "Identity provider must support PKCE S256.")
        return m

    async def start(self, pending):
        m = await self.metadata()
        challenge = base64.urlsafe_b64encode(hashlib.sha256(pending["verifier"].encode()).digest()).rstrip(b"=").decode()
        return m["authorization_endpoint"] + "?" + urlencode({"client_id": self.s.client_id, "redirect_uri": self.s.public_origin + "/callback",
            "response_type": "code", "scope": "openid", "state": pending["state"], "nonce": pending["nonce"],
            "code_challenge": challenge, "code_challenge_method": "S256"})

    async def finish(self, pending, code):
        m = await self.metadata()
        tokens = await self.transport.json(m["token_endpoint"], "POST", form={"grant_type": "authorization_code", "code": code,
            "client_id": self.s.client_id, "redirect_uri": self.s.public_origin + "/callback", "code_verifier": pending["verifier"]})
        keys = (await self.transport.json(m["jwks_uri"])).get("keys", [])
        try:
            if tokens.get("token_type", "").lower() != "bearer":
                raise ValueError()
            claims = []
            for name, aud in (("id_token", self.s.client_id), ("access_token", self.s.audience)):
                token = tokens[name]
                if not isinstance(token, str) or len(token) > 65536:
                    raise ValueError()
                header = jwt.get_unverified_header(token)
                matching = [k for k in keys if k.get("kid") == header.get("kid") and k.get("kty") == "RSA" and k.get("use", "sig") == "sig" and k.get("alg", "RS256") == "RS256"]
                if len(matching) != 1 or not header.get("kid") or header.get("alg") != "RS256":
                    raise ValueError()
                key = jwt.PyJWK.from_dict(matching[0], algorithm="RS256").key
                if key.key_size < 2048:
                    raise ValueError()
                c = jwt.decode(token, key, algorithms=["RS256"], audience=aud, issuer=self.s.issuer,
                               options={"require": ["exp", "iat", "iss", "aud", "sub"]})
                if not 0 < c["exp"] - c["iat"] <= 3600 or c["iat"] > time.time() or not c["sub"]:
                    raise ValueError()
                claims.append(c)
            identity, access = claims
            if identity.get("nonce") != pending["nonce"] or identity["sub"] != access["sub"]:
                raise ValueError()
            if (isinstance(identity["aud"], list) and len(identity["aud"]) > 1 or "azp" in identity) and identity.get("azp") != self.s.client_id:
                raise ValueError()
            if "at_hash" in identity:
                expected = base64.urlsafe_b64encode(hashlib.sha256(tokens["access_token"].encode()).digest()[:16]).rstrip(b"=").decode()
                if not secrets.compare_digest(identity["at_hash"], expected):
                    raise ValueError()
            return Session(tokens["access_token"], uuid7(access["sub"]), uuid7(access[self.s.tenant_claim]), min(identity["exp"], access["exp"], time.time() + 1800))
        except (jwt.PyJWTError, ValueError, KeyError, TypeError, AttributeError):
            raise Failure(401, "Identity verification failed. Sign in again.") from None
