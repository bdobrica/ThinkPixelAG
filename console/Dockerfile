FROM python:3.13.15-alpine3.24@sha256:1a63a53928ce53d2b0baf08092a703f4840ac5dfbd61fd48802dbf48e08c801e AS dependencies
WORKDIR /build
COPY requirements.txt ./
RUN pip install --no-cache-dir --require-hashes --only-binary=:all: --target /packages -r requirements.txt

FROM python:3.13.15-alpine3.24@sha256:1a63a53928ce53d2b0baf08092a703f4840ac5dfbd61fd48802dbf48e08c801e
ARG VERSION=dev
ARG REVISION=unknown
ARG CREATED=unknown
LABEL org.opencontainers.image.title="ThinkPixelAG Console" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$REVISION \
      org.opencontainers.image.created=$CREATED
# The runtime does not install packages. Remove the packaging tools (including
# their vendored dependencies); retain the installed application dependency metadata.
RUN pip uninstall -y pip setuptools wheel
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY --from=dependencies /packages /usr/local/lib/python3.13/site-packages
COPY ag_console ./ag_console
USER 65532:65532
EXPOSE 8081
CMD ["python", "-m", "uvicorn", "ag_console.app:create_app", "--factory", "--host", "0.0.0.0", "--port", "8081", "--workers", "1", "--no-access-log", "--no-proxy-headers", "--limit-concurrency", "64", "--timeout-keep-alive", "5"]
