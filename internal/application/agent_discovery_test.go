package application

import (
	"context"
	"errors"
	"testing"

	"github.com/bdobrica/ThinkPixelAG/internal/domain"
	"github.com/bdobrica/ThinkPixelAG/internal/policy"
	"github.com/bdobrica/ThinkPixelAG/internal/ports"
)

type discoveryRepositoryStub struct {
	agents      []domain.Agent
	candidates  map[domain.ID][]domain.AgentVersionCandidate
	describe    domain.Agent
	describeErr error
}

func (s *discoveryRepositoryStub) DescribeAgent(context.Context, domain.ID) (domain.Agent, error) {
	return s.describe, s.describeErr
}
func (s *discoveryRepositoryStub) ListAgents(_ context.Context, q ports.AgentListQuery) ([]domain.Agent, error) {
	out := make([]domain.Agent, 0, q.Limit)
	for _, a := range s.agents {
		if q.After.IsZero() || a.ID.String() > q.After.String() {
			out = append(out, a)
			if len(out) == q.Limit {
				break
			}
		}
	}
	return out, nil
}
func (s *discoveryRepositoryStub) ListAgentVersionCandidates(_ context.Context, id domain.ID, _ string) ([]domain.AgentVersionCandidate, error) {
	return s.candidates[id], nil
}

type discoveryEvaluatorStub struct {
	denied  map[string]bool
	fail    bool
	actions []string
}

func (s *discoveryEvaluatorStub) Decide(_ context.Context, in policy.Input) (policy.Result, error) {
	s.actions = append(s.actions, in.Action+":"+in.Resource.ID)
	if s.fail {
		return policy.Result{}, errors.New("policy down")
	}
	return policy.Result{Decision: policy.Decision{DecisionID: in.DecisionID, Allow: !s.denied[in.Resource.ID], ResolvedConstraints: map[string]any{}}}, nil
}

func TestAgentDiscoveryFiltersAndPaginatesAuthorizedInvocableAgents(t *testing.T) {
	t.Parallel()
	first, denied, last := resolutionCandidate(t, domain.AgentVersionApproved, "1"), resolutionCandidate(t, domain.AgentVersionApproved, "2"), resolutionCandidate(t, domain.AgentVersionApproved, "3")
	// UUIDv7 generation order is not guaranteed within one millisecond; sort the
	// complete candidate records by their public pagination key.
	candidates := []domain.AgentVersionCandidate{first, denied, last}
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Agent.ID.String() < candidates[i].Agent.ID.String() {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	first, denied, last = candidates[0], candidates[1], candidates[2]
	repo := &discoveryRepositoryStub{agents: []domain.Agent{first.Agent, denied.Agent, last.Agent}, candidates: map[domain.ID][]domain.AgentVersionCandidate{first.Agent.ID: {first}, denied.Agent.ID: {denied}, last.Agent.ID: {last}}}
	evaluator := &discoveryEvaluatorStub{denied: map[string]bool{denied.Agent.ID.String(): true}}
	service, _ := NewAgentDiscovery(repo, evaluator, fixedClock{now: testTime()})
	command := DiscoverAgents{TenantID: first.Agent.TenantID, PrincipalID: applicationID(t), RequestID: applicationID(t), Roles: []string{"agent-invoker"}, Limit: 1}
	// Put all records under the command tenant to exercise filtering rather than
	// the authority-boundary guard.
	for i := range repo.agents {
		repo.agents[i].TenantID = command.TenantID
		c := repo.candidates[repo.agents[i].ID][0]
		c.Agent.TenantID, c.Version.TenantID, c.Approval.TenantID = command.TenantID, command.TenantID, command.TenantID
		repo.candidates[repo.agents[i].ID] = []domain.AgentVersionCandidate{c}
	}
	page, err := service.List(context.Background(), command)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != first.Agent.ID || page.Next != first.Agent.ID {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	command.After = page.Next
	page, err = service.List(context.Background(), command)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != last.Agent.ID || !page.Next.IsZero() {
		t.Fatalf("second page=%+v err=%v", page, err)
	}
}

func TestAgentDiscoveryDescribeIsEnumerationSafeAndFailsClosed(t *testing.T) {
	t.Parallel()
	candidate := resolutionCandidate(t, domain.AgentVersionApproved, "4")
	repo := &discoveryRepositoryStub{describe: candidate.Agent, candidates: map[domain.ID][]domain.AgentVersionCandidate{candidate.Agent.ID: {candidate}}}
	evaluator := &discoveryEvaluatorStub{denied: map[string]bool{candidate.Agent.ID.String(): true}}
	service, _ := NewAgentDiscovery(repo, evaluator, fixedClock{now: testTime()})
	command := DiscoverAgents{TenantID: candidate.Agent.TenantID, PrincipalID: applicationID(t), RequestID: applicationID(t), Limit: 1}
	_, err := service.Describe(context.Background(), command, candidate.Agent.ID)
	if domain.ErrorCodeOf(err) != domain.CodeNotFound {
		t.Fatalf("denied describe=%v", err)
	}
	evaluator.fail = true
	_, err = service.Describe(context.Background(), command, candidate.Agent.ID)
	if domain.ErrorCodeOf(err) != domain.CodeUnavailable {
		t.Fatalf("policy outage=%v", err)
	}
	repo.describeErr = domain.NewError(domain.CodeNotFound, "different hidden detail")
	evaluator.fail = false
	_, err = service.Describe(context.Background(), command, candidate.Agent.ID)
	if domain.ErrorCodeOf(err) != domain.CodeNotFound || err.Error() == "different hidden detail" {
		t.Fatalf("missing describe=%v", err)
	}
}
