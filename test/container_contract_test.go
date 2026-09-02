package repository_test

import (
	"os"
	"strings"
	"testing"
)

func TestDockerfileIsPinnedMinimalAndNonRoot(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(data)
	for _, required := range []string{
		"golang:1.26.6-alpine3.23@sha256:",
		"gcr.io/distroless/static-debian13:nonroot@sha256:",
		"CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH",
		"-trimpath -buildvcs=false",
		"USER 65532:65532",
		`ENTRYPOINT ["/thinkpixelag"]`,
		"org.opencontainers.image.version=$VERSION",
		"org.opencontainers.image.revision=$REVISION",
		"org.opencontainers.image.created=$CREATED",
		"/out/thinkpixelag-migrate /thinkpixelag-migrate",
		"/src/migrations /migrations",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile contract %q is missing", required)
		}
	}
	if strings.Contains(dockerfile, "latest") {
		t.Error("Dockerfile must not use a latest tag")
	}
}

func TestDockerContextIsAllowlisted(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("../.dockerignore")
	if err != nil {
		t.Fatal(err)
	}
	ignore := string(data)
	if !strings.HasPrefix(ignore, "*\n") {
		t.Error("Docker context must deny files by default")
	}
	for _, allowed := range []string{"!go.mod", "!go.sum", "!cmd/**", "!internal/**"} {
		if !strings.Contains(ignore, allowed) {
			t.Errorf("Docker context allowlist %q is missing", allowed)
		}
	}
}
