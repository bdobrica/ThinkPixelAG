package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
	"github.com/jackc/pgx/v5"
)

var _ ports.VersionResolutionRepository = (*TenantRepository)(nil)

func (r *TenantRepository) ListAgentVersionCandidates(ctx context.Context, agentID domain.ID, requestedDigest string) ([]domain.AgentVersionCandidate, error) {
	if err := r.valid(); err != nil {
		return nil, err
	}
	if agentID.IsZero() || requestedDigest != "" && !domain.ValidDigest(requestedDigest) {
		return nil, errors.New("version candidate query is invalid")
	}
	rows, err := r.db.Query(ctx, `SELECT
 a.id::text,a.tenant_id::text,a.name,a.description,a.owner_principal_id::text,a.sponsor_principal_id::text,a.risk_class,a.status,a.created_at,a.updated_at,
 v.id::text,v.content_digest,v.image_digest,v.manifest,v.created_by::text,v.created_at,
 approved.id::text,approved.actor_principal_id::text,approved.policy_decision_id::text,approved.reason_code,COALESCE(approved.approval_reference,''),approved.created_at,current_state.decision
FROM agents a JOIN agent_versions v ON v.tenant_id=a.tenant_id AND v.agent_id=a.id
JOIN LATERAL (SELECT decision FROM agent_version_approvals s WHERE s.tenant_id=v.tenant_id AND s.agent_version_id=v.id ORDER BY s.created_at DESC,s.id DESC LIMIT 1) current_state ON true
JOIN LATERAL (SELECT id,actor_principal_id,policy_decision_id,reason_code,approval_reference,created_at FROM agent_version_approvals p WHERE p.tenant_id=v.tenant_id AND p.agent_version_id=v.id AND p.decision='APPROVED' ORDER BY p.created_at DESC,p.id DESC LIMIT 1) approved ON true
WHERE a.tenant_id=$1 AND a.id=$2 AND a.status='ACTIVE'
  AND (($3='' AND current_state.decision='APPROVED') OR ($3<>'' AND v.content_digest=$3 AND current_state.decision IN ('APPROVED','DEPRECATED')))
ORDER BY v.created_at DESC,v.id DESC`, r.tenantID.String(), agentID.String(), requestedDigest)
	if err != nil {
		return nil, fmt.Errorf("list version resolution candidates: %w", err)
	}
	defer rows.Close()
	result := make([]domain.AgentVersionCandidate, 0)
	for rows.Next() {
		var candidate domain.AgentVersionCandidate
		var tenantID, agentIDText, ownerID, sponsorID, versionID, creatorID, approvalID, actorID, policyID, risk, agentStatus, state string
		var manifest []byte
		if err := rows.Scan(&agentIDText, &tenantID, &candidate.Agent.Name, &candidate.Agent.Description, &ownerID, &sponsorID, &risk, &agentStatus, &candidate.Agent.CreatedAt, &candidate.Agent.UpdatedAt,
			&versionID, &candidate.Version.ContentDigest, &candidate.Version.ImageDigest, &manifest, &creatorID, &candidate.Version.CreatedAt,
			&approvalID, &actorID, &policyID, &candidate.Approval.ReasonCode, &candidate.Approval.ApprovalReference, &candidate.Approval.CreatedAt, &state); err != nil {
			return nil, fmt.Errorf("scan version resolution candidate: %w", err)
		}
		parse := func(value string, target *domain.ID) error {
			parsed, parseErr := domain.ParseID(value)
			*target = parsed
			return parseErr
		}
		if parse(tenantID, &candidate.Agent.TenantID) != nil || parse(agentIDText, &candidate.Agent.ID) != nil || parse(ownerID, &candidate.Agent.OwnerPrincipalID) != nil || parse(sponsorID, &candidate.Agent.SponsorPrincipalID) != nil || parse(versionID, &candidate.Version.ID) != nil || parse(creatorID, &candidate.Version.CreatedBy) != nil || parse(approvalID, &candidate.Approval.ID) != nil || parse(actorID, &candidate.Approval.ActorPrincipalID) != nil || parse(policyID, &candidate.Approval.PolicyDecisionID) != nil {
			return nil, errors.New("decode version resolution candidate identifier")
		}
		candidate.Agent.RiskClass, candidate.Agent.Status = domain.AgentRiskClass(risk), domain.AgentStatus(agentStatus)
		candidate.Agent.CreatedAt, candidate.Agent.UpdatedAt = candidate.Agent.CreatedAt.UTC(), candidate.Agent.UpdatedAt.UTC()
		candidate.Version.TenantID, candidate.Version.AgentID, candidate.Version.CreatedAt = candidate.Agent.TenantID, candidate.Agent.ID, candidate.Version.CreatedAt.UTC()
		candidate.Version.Manifest, err = domain.ParseAgentManifest(manifest)
		if err != nil {
			return nil, fmt.Errorf("decode candidate manifest: %w", err)
		}
		candidate.Approval.TenantID, candidate.Approval.AgentID, candidate.Approval.AgentVersionID = candidate.Agent.TenantID, candidate.Agent.ID, candidate.Version.ID
		candidate.Approval.Decision, candidate.Approval.CreatedAt = domain.DecisionApprove, candidate.Approval.CreatedAt.UTC()
		candidate.State = domain.AgentVersionState(state)
		if err := candidate.Agent.Validate(); err != nil {
			return nil, fmt.Errorf("validate candidate agent: %w", err)
		}
		if err := candidate.Version.Validate(); err != nil {
			return nil, fmt.Errorf("validate candidate version: %w", err)
		}
		if err := candidate.Approval.Validate(); err != nil {
			return nil, fmt.Errorf("validate candidate approval: %w", err)
		}
		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate version resolution candidates: %w", err)
	}
	return result, nil
}

type storedResolutionEvidence struct {
	Mode                 string         `json:"mode"`
	InvocationDecisionID string         `json:"invocation_decision_id"`
	SelectionDecisionID  string         `json:"selection_decision_id,omitempty"`
	ResolvedConstraints  map[string]any `json:"resolved_constraints"`
}

func (r *TenantRepository) PersistRunVersionResolution(ctx context.Context, resolution domain.RunVersionResolution) error {
	if err := r.valid(); err != nil {
		return err
	}
	if resolution.TenantID != r.tenantID {
		return errors.New("version resolution tenant does not match repository scope")
	}
	if err := resolution.Validate(); err != nil {
		return err
	}
	evidence := storedResolutionEvidence{Mode: string(resolution.Mode), InvocationDecisionID: resolution.InvocationDecisionID.String(), ResolvedConstraints: resolution.ResolvedConstraints}
	if !resolution.SelectionDecisionID.IsZero() {
		evidence.SelectionDecisionID = resolution.SelectionDecisionID.String()
	}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("encode version resolution evidence: %w", err)
	}
	if len(payload) > 64<<10 {
		return domain.NewError(domain.CodeInvalidArgument, "version resolution evidence exceeds bounds")
	}
	var inserted int
	err = r.db.QueryRow(ctx, `WITH locked_version AS (
 SELECT id,content_digest FROM agent_versions WHERE tenant_id=$2 AND agent_id=$3 AND id=$4 AND content_digest=$5 FOR UPDATE
), eligible_version AS (
 SELECT locked_version.id FROM locked_version JOIN LATERAL (SELECT decision FROM agent_version_approvals state WHERE state.tenant_id=$2 AND state.agent_version_id=locked_version.id ORDER BY state.created_at DESC,state.id DESC LIMIT 1) current_state ON true
 WHERE current_state.decision='APPROVED' OR ($11='ROLLBACK' AND current_state.decision='DEPRECATED')
), inserted AS (
 INSERT INTO run_version_resolutions(run_id,tenant_id,agent_id,agent_version_id,agent_content_digest,policy_bundle_digest,policy_activation_version,approval_id,resolution,resolved_at)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb,$10 FROM runs run
 JOIN eligible_version ON eligible_version.id=run.agent_version_id
 JOIN agent_version_approvals approval ON approval.tenant_id=run.tenant_id AND approval.id=$8 AND approval.agent_id=$3 AND approval.agent_version_id=$4 AND approval.decision='APPROVED'
 WHERE run.tenant_id=$2 AND run.id=$1 AND run.agent_id=$3 AND run.agent_version_id=$4 RETURNING run_id
) SELECT count(*) FROM inserted`, resolution.RunID.String(), r.tenantID.String(), resolution.AgentID.String(), resolution.AgentVersionID.String(), resolution.AgentContentDigest, resolution.PolicyBundleDigest, resolution.PolicyActivationVersion, resolution.ApprovalID.String(), payload, resolution.ResolvedAt, string(resolution.Mode)).Scan(&inserted)
	if err == nil && inserted == 0 {
		return domain.NewError(domain.CodeNotFound, "matching run and approved version not found")
	}
	if err != nil {
		if ClassifyError(err) == ErrorUniqueViolation {
			return domain.WrapError(domain.CodeConflict, "run version resolution already exists", err)
		}
		return fmt.Errorf("persist run version resolution: %w", err)
	}
	return nil
}

func (r *TenantRepository) DescribeRunVersionResolution(ctx context.Context, runID domain.ID) (domain.RunVersionResolution, error) {
	if err := r.valid(); err != nil {
		return domain.RunVersionResolution{}, err
	}
	var result domain.RunVersionResolution
	var run, tenant, agent, version, approval string
	var evidenceJSON []byte
	err := r.db.QueryRow(ctx, `SELECT run_id::text,tenant_id::text,agent_id::text,agent_version_id::text,approval_id::text,agent_content_digest,policy_bundle_digest,policy_activation_version,resolution,resolved_at FROM run_version_resolutions WHERE tenant_id=$1 AND run_id=$2`, r.tenantID.String(), runID.String()).Scan(&run, &tenant, &agent, &version, &approval, &result.AgentContentDigest, &result.PolicyBundleDigest, &result.PolicyActivationVersion, &evidenceJSON, &result.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RunVersionResolution{}, domain.NewError(domain.CodeNotFound, "run version resolution not found")
	}
	if err != nil {
		return domain.RunVersionResolution{}, fmt.Errorf("describe run version resolution: %w", err)
	}
	for _, pair := range []struct {
		value  string
		target *domain.ID
	}{{run, &result.RunID}, {tenant, &result.TenantID}, {agent, &result.AgentID}, {version, &result.AgentVersionID}, {approval, &result.ApprovalID}} {
		parsed, parseErr := domain.ParseID(pair.value)
		if parseErr != nil {
			return domain.RunVersionResolution{}, fmt.Errorf("decode run version resolution ID: %w", parseErr)
		}
		*pair.target = parsed
	}
	if len(evidenceJSON) > 64<<10 {
		return domain.RunVersionResolution{}, errors.New("stored run version resolution evidence exceeds bounds")
	}
	var evidence storedResolutionEvidence
	decoder := json.NewDecoder(bytes.NewReader(evidenceJSON))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return domain.RunVersionResolution{}, fmt.Errorf("decode run version resolution evidence: %w", err)
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return domain.RunVersionResolution{}, errors.New("stored run version resolution evidence contains trailing JSON")
	}
	result.Mode, result.ResolvedConstraints, result.ResolvedAt = domain.VersionResolutionMode(evidence.Mode), evidence.ResolvedConstraints, result.ResolvedAt.UTC()
	if result.InvocationDecisionID, err = domain.ParseID(evidence.InvocationDecisionID); err != nil {
		return domain.RunVersionResolution{}, fmt.Errorf("decode invocation decision ID: %w", err)
	}
	if evidence.SelectionDecisionID != "" {
		if result.SelectionDecisionID, err = domain.ParseID(evidence.SelectionDecisionID); err != nil {
			return domain.RunVersionResolution{}, fmt.Errorf("decode selection decision ID: %w", err)
		}
	}
	if err := result.Validate(); err != nil {
		return domain.RunVersionResolution{}, fmt.Errorf("validate stored run version resolution: %w", err)
	}
	return result, nil
}
