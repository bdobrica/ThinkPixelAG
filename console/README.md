# Optional administration console

A separate FastAPI/Jinja2 application that calls AG's published APIs using the
signed-in operator's token. AG runs without this package, its image or Python.
The BFF has no database, OPA, signing-key or downstream provider access.

## Install and sign in

Install the current source candidate using [operator bootstrap](../docs/operations/bootstrap.md).
The published `0.1.0-rc.1` image predates these administration APIs. Supply HTTPS
origins for AG, the console and your OIDC issuer; a locally trusted development
CA is supported. Never disable TLS verification.

Register a **public OIDC client** with your issuer:

- Authorization code flow and PKCE S256, with `openid` scope.
- Exact redirect URI `https://console.example.test/callback`; response mode query.
- RS256 ID tokens with the console client ID as audience and a nonce.
- RS256 JWT access tokens with AG's configured audience and verified UUIDv7
  principal/tenant claims, matching the ID-token subject. Maximum lifetime one hour.
- Configure the issuer to issue that AG audience for this client. This RC has no
  token exchange, client-secret flow or provider-specific audience query override.
- Discovery/token/JWKS/authorization endpoints must share the configured issuer's
  HTTPS origin. Different-origin IdP deployments need a reviewed adapter change.

Set deployment configuration (these values contain no caller tokens):

```sh
export AG_CONSOLE_PUBLIC_ORIGIN=https://console.example.test
export AG_CONSOLE_AG_ORIGIN=https://ag.example.test
export AG_CONSOLE_ISSUER=https://identity.example.test/realms/evaluation
export AG_CONSOLE_CLIENT_ID=thinkpixelag-console
export AG_CONSOLE_AUDIENCE=thinkpixelag
# Optional: private CA trust and a nondefault verified tenant claim.
export AG_CONSOLE_CA_FILE=/absolute/path/to/ca.pem
export AG_CONSOLE_TENANT_CLAIM=tenant_id
```

Use Python 3.13. From the repository root:

```sh
python3 -m venv .cache/console/venv
.cache/console/venv/bin/pip install --require-hashes -r console/requirements.txt
PYTHONPATH=console .cache/console/venv/bin/python -m uvicorn ag_console.app:create_app \
  --factory --host 127.0.0.1 --port 8081 --workers 1 --no-access-log --no-proxy-headers
```

Put an HTTPS reverse proxy in front of that private listener, preserving the
public Host header. Do not expose the plaintext listener publicly. Alternatively
use Uvicorn's `--ssl-certfile`/`--ssl-keyfile` for local HTTPS. The application
uses the configured public origin, never forwarded headers, for redirects and
form-origin checks. Origins have no trailing slash/path. The issuer may include
a realm path, without a trailing slash.

Open the public console origin and choose **Sign in**. Authentication occurs at
the IdP. The console retains caller tokens only in server memory; browser cookies
contain opaque random handles, not tokens. Configure the proxy to omit callback
queries, cookies, Authorization headers and bodies from logs. Uvicorn access
logging is disabled so authorization codes never enter access logs.

The overview distinguishes configuration from readiness; each card is authorized
independently. Agents/Runs show only what AG permits, with pagination and detail
views. An operator lacking a particular role sees denial rather than a shared
administrator identity. The OPA status check runs through AG, not the BFF.

## Container and operating limits

```sh
make console-image
# Supply the AG_CONSOLE_* settings above through a deployment-owned env file.
docker run --name thinkpixelag-console --read-only --cap-drop ALL \
  --security-opt no-new-privileges:true --env-file /private/console.env \
  -p 127.0.0.1:8081:8081 thinkpixelag-console:dev
```

If using a private CA, mount that public CA certificate read-only at the configured
path. Mount no database credentials, policy keys or provider secrets. The image
runs as UID 65532. Set the reverse proxy's upstream Host to the configured public
host, including its port when nonstandard. `/healthz` checks only console liveness.

Run **one replica and one worker** for this RC. Sessions expire at the earlier of
30 minutes or either token's expiry; sign in again, rather than retaining refresh
tokens. At most 1,000 sessions and 1,000 five-minute pending logins are retained.
Restart or logout destroys console session state; it does not change AG state or
log the user out of the IdP. Upstream calls have a 12-second total bound; responses
are limited to two MiB, browser form bodies to one MiB. No ambient HTTP proxy or
upstream redirect is followed. Harden host/network access independently of the UI.

Local software signing and local approval custody retain [ADR-0016's limits](../docs/adr/0016-local-development-policy-promotion.md).
No external IdP, production KMS/HSM or console HA qualification is implied.

## Developer checks and dependency updates

```sh
.cache/console/venv/bin/pip install --require-hashes -r console/requirements-test.txt
make test-console
make verify-console
```

`make verify` remains the AG service gate; `make verify-console` is the independent
optional package gate. Browser acceptance uses Playwright only in the test host.
The runtime dependencies have specific purposes: FastAPI/Starlette serve ASGI,
Jinja2 escapes views, HTTPX bounds transport, PyJWT/cryptography verify issuer
signatures, and Uvicorn serves the separate process. No Python dependency enters
AG's Go build or image.

`requirements.in` is the source; generated locks pin transitive dependencies and
hashes. Update using `pip-tools==7.6.1` on Python 3.13:

```sh
pip-compile --generate-hashes --strip-extras --output-file console/requirements.txt console/requirements.in
pip-compile --generate-hashes --strip-extras --output-file console/requirements-test.txt console/requirements-test.in
```
