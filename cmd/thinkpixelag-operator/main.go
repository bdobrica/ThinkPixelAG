// Command thinkpixelag-operator is a protected, local-development operator tool.
// Database credentials and mounted files are its deployment authority. No HTTP
// route exposes bootstrap or recovery.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/integrationconfig"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localkeys"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/opa"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if e := run(context.Background(), os.Args[1:], os.Stdout); e != nil {
		fmt.Fprintln(os.Stderr, "thinkpixelag-operator:", e)
		os.Exit(1)
	}
}
func readPrivate(path string, max int64) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("operator input must be a private regular file")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("operator input unavailable")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, errors.New("operator input exceeds bounds")
	}
	return b, nil
}
func decode(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("operator specification is invalid")
	}
	return nil
}
func run(ctx context.Context, args []string, out io.Writer) error {
	if os.Getenv("THINKPIXELAG_ENVIRONMENT") != "local" && os.Getenv("THINKPIXELAG_ENVIRONMENT") != "test" {
		return errors.New("operator command requires explicit local/test environment")
	}
	if len(args) == 0 {
		return errors.New("expected bootstrap or recovery command")
	}
	if args[0] == "new-id" && len(args) == 1 {
		id, e := domain.NewID()
		if e != nil {
			return e
		}
		_, e = fmt.Fprintln(out, id.String())
		return e
	}
	if args[0] != "bootstrap" {
		return runRecovery(ctx, args, out)
	}
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repair := fs.Bool("repair-resource-catalog", false, "explicitly initialize missing root resource definitions on an identical bootstrap receipt")
	specPath := fs.String("spec", "", "private reviewed initial snapshot")
	policyPath := fs.String("policy", "", "reviewed Rego source")
	keyPath := fs.String("key", "", "private persistent local signing key")
	origin := fs.String("opa-origin", "", "deployment-approved exact OPA origin")
	tokenPath := fs.String("opa-token-file", "", "optional private OPA token file")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *specPath == "" || *policyPath == "" || *keyPath == "" || *origin == "" {
		return errors.New("bootstrap requires --spec --policy --key --opa-origin")
	}
	raw, e := readPrivate(*specPath, 1<<20)
	if e != nil {
		return e
	}
	var spec ports.BootstrapSpec
	if e = decode(raw, &spec); e != nil {
		return e
	}
	spec.RepairResourceCatalog = *repair
	source, e := os.ReadFile(*policyPath)
	if e != nil || len(source) > 1<<20 {
		return errors.New("reviewed policy source unavailable or exceeds bounds")
	}
	key, e := localkeys.Provision(*keyPath)
	if e != nil {
		return errors.New("local signing key unavailable")
	}
	files := map[string]string{}
	if *tokenPath != "" {
		files[spec.OPA.TokenReference] = *tokenPath
	}
	resolver := integrationconfig.OPA{AllowedOrigins: []string{*origin}, SecretFiles: files, Client: http.DefaultClient, Timeout: 5 * time.Second}
	modules, e := resolver.Modules(spec.OPA)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("THINKPIXELAG_DATABASE_URL"))
	if e != nil {
		return errors.New("operator database configuration invalid")
	}
	defer pool.Close()
	repos, e := postgres.NewRepositories(pool)
	if e != nil {
		return e
	}
	repo, e := repos.ForTenant(spec.TenantID)
	if e != nil {
		return e
	}
	service := application.OperatorBootstrap{Store: repo, Signer: key, Verifier: key, KeyID: key.ID(), Modules: modules, Clock: domain.SystemClock{}, Evaluator: func(store ports.PolicyAdministrationStore) policy.Evaluator {
		return &opa.ArtifactEvaluator{Store: store, Modules: modules, Verifier: key, Channel: spec.Channel, MaxTTL: time.Minute}
	}}
	result, e := service.Provision(ctx, spec, source)
	if e != nil {
		return fmt.Errorf("bootstrap failed (%s); existing authority was not replaced", domain.ErrorCodeOf(e))
	}
	return json.NewEncoder(out).Encode(result)
}
