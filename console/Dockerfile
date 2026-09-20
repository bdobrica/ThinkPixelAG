FROM python:3.13.12-slim-bookworm@sha256:a58daefb915e1e03ad48f3ca4df8832065412c5c35cacb9d39f4229184de12b6
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir --require-hashes -r requirements.txt
COPY ag_console ./ag_console
USER 65532:65532
EXPOSE 8081
CMD ["python", "-m", "uvicorn", "ag_console.app:create_app", "--factory", "--host", "0.0.0.0", "--port", "8081", "--workers", "1", "--no-access-log", "--no-proxy-headers", "--limit-concurrency", "64", "--timeout-keep-alive", "5"]
