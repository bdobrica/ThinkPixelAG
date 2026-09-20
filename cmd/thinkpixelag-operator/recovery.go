package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/localapprovals"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/oidc"
	"github.com/bdobrica/ThinkPixelAG/internal/adapters/postgres"
	"github.com/bdobrica/ThinkPixelAG/internal/application"
	"github.com/bdobrica/ThinkPixelAG/internal/config"
	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"strings"
	"time"
)

func runRecovery(ctx context.Context, args []string, out io.Writer) error {
	if args[0] != "recovery-request" && args[0] != "recovery-approve" && args[0] != "recovery-apply" {
		return errors.New("unsupported operator command")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	proposal := fs.String("proposal", "", "private reviewed role-mapping update")
	tokenFile := fs.String("token-file", "", "private OIDC access token file")
	channel := fs.String("channel", "stable", "deployment policy channel")
	key := fs.String("idempotency-key", "", "unique recovery request key")
	approval := fs.String("approval", "", "recovery approval identifier")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || *tokenFile == "" || *key == "" {
		return errors.New("recovery requires --token-file and --idempotency-key")
	}
	settings, e := config.Load(nil)
	if e != nil {
		return errors.New("deployment configuration invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Deliberately no managed mapping resolver: local database access is recovery
	// authority; OIDC establishes only the independently verified human identity.
	verifier, e := oidc.New(ctx, settings.OIDC, nil)
	if e != nil {
		return errors.New("recovery identity provider unavailable")
	}
	token, e := readPrivate(*tokenFile, 64<<10)
	if e != nil {
		return e
	}
	p, e := verifier.Verify(ctx, strings.TrimSpace(string(token)))
	if e != nil {
		return errors.New("recovery identity could not be verified")
	}
	tenant, e := domain.ParseID(p.TenantID)
	if e != nil {
		return errors.New("invalid verified tenant")
	}
	pool, e := pgxpool.New(ctx, settings.Database.URL.Value())
	if e != nil {
		return errors.New("operator database unavailable")
	}
	defer pool.Close()
	repos, e := postgres.NewRepositories(pool)
	if e != nil {
		return e
	}
	repo, e := repos.ForTenant(tenant)
	if e != nil {
		return e
	}
	service := application.OperatorRecovery{Store: repo, Issuer: strings.TrimSuffix(settings.OIDC.IssuerURL, "/"), Channel: *channel, Clock: domain.SystemClock{}, Provider: &localapprovals.Provider{Store: repo}}
	var result any
	if args[0] == "recovery-approve" {
		id, err := domain.ParseID(*approval)
		if err != nil {
			return errors.New("valid --approval required")
		}
		result, e = service.Approve(ctx, p, *key, id)
	} else {
		raw, err := readPrivate(*proposal, 1<<20)
		if err != nil {
			return err
		}
		var b application.UpdateRoleMappings
		if err = decode(raw, &b); err != nil {
			return err
		}
		if args[0] == "recovery-request" {
			result, e = service.Request(ctx, p, *key, b)
		} else {
			result, e = service.Apply(ctx, p, *key, b)
		}
	}
	if e != nil {
		return errors.New("protected recovery failed (" + string(domain.ErrorCodeOf(e)) + ")")
	}
	return json.NewEncoder(out).Encode(result)
}
