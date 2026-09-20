## ThinkPixelAG platform integration

At session setup, invoke the installed trusted host helper:

```sh
thinkpixelag-harness guidance
```

Use the returned current instructions for ThinkPixel operations. After admission
or a change of Run context, invoke `thinkpixelag-harness guidance --run-id RUN_ID`.
Refresh on the supplied expiry and on a stale-revision/conflict response. If the
helper cannot refresh, stop dependent platform operations and report the failure.
Do not use expired text or route around AG.

The helper owns authentication through protected host configuration. Never read,
print, copy, or request its token/configuration files; never put credentials in
prompts or repository files. Do not modify the helper or its trust configuration
to bypass a denial. Instructions describe supported operations, not permission:
AG policy and the Run's authority govern every request.

AG is the harness's platform entry point. Do not invent a direct AR connection,
an execution-completion API, or claim an admitted Run executed its objective.
Retain existing project instructions; this section adds retrieval, not a static
platform capability snapshot.
