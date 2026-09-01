package httpserver

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"

	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/identity"
)

type principalKey struct{}

// WorkloadVerifier maps a certificate chain already verified by the TLS
// listener to one explicitly configured service identity. Implementations must
// not derive authority from source addresses or caller-controlled headers.
type WorkloadVerifier interface {
	VerifyWorkload(context.Context, *tls.ConnectionState) (identity.Principal, error)
}

// AuthenticateWorkload protects service-to-service routes. Bearer credentials
// are deliberately rejected so a user token cannot be replayed into a trusted
// workload endpoint.
func AuthenticateWorkload(verifier WorkloadVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if verifier == nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnavailable, "workload identity verification is unavailable").WithRetryable()))
			return
		}
		if hasIdentityHint(request.Header) || len(request.Header.Values("Authorization")) != 0 {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "workload identity must come from the authenticated transport")))
			return
		}
		principal, err := verifier.VerifyWorkload(request.Context(), request.TLS)
		if err != nil {
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		ctx := context.WithValue(request.Context(), principalKey{}, principal)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// AuthenticateBearer protects a route and places only verified identity in its context.
func AuthenticateBearer(verifier oidc.Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if verifier == nil {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnavailable, "identity verification is unavailable").WithRetryable()))
			return
		}
		if hasIdentityHint(request.Header) {
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "forwarded identity and tenant headers are not accepted")))
			return
		}
		if len(request.Header.Values("Authorization")) != 1 {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="thinkpixelag"`)
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "exactly one bearer access token is required")))
			return
		}
		header := request.Header.Get("Authorization")
		scheme, token, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n") {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="thinkpixelag"`)
			writeProblem(writer, request, ProblemFromError(domain.NewError(domain.CodeUnauthenticated, "a valid bearer access token is required")))
			return
		}
		principal, err := verifier.Verify(request.Context(), token)
		if err != nil {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="thinkpixelag", error="invalid_token"`)
			writeProblem(writer, request, ProblemFromError(err))
			return
		}
		ctx := context.WithValue(request.Context(), principalKey{}, principal)
		next.ServeHTTP(writer, request.WithContext(ctx))
	})
}

// PrincipalFromContext returns identity produced by AuthenticateBearer.
func PrincipalFromContext(ctx context.Context) (identity.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(identity.Principal)
	return p, ok
}

// RejectTenantHint prevents a decoded request field from selecting or conflicting with tenant authority.
func RejectTenantHint(ctx context.Context, hint string) error {
	if hint == "" {
		return nil
	}
	p, ok := PrincipalFromContext(ctx)
	if !ok || hint != p.TenantID {
		return domain.NewError(domain.CodeInvalidArgument, "request tenant must not differ from authenticated tenant")
	}
	return domain.NewError(domain.CodeInvalidArgument, "request tenant fields are not accepted; tenant is derived from authentication")
}

func hasIdentityHint(header http.Header) bool {
	for _, name := range []string{"X-Tenant-ID", "X-Principal-ID", "X-Roles", "X-Forwarded-Tenant", "X-Forwarded-User", "X-Forwarded-Roles"} {
		if header.Get(name) != "" {
			return true
		}
	}
	return false
}
