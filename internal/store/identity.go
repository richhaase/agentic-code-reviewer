package store

import (
	"fmt"
	"strings"

	"github.com/richhaase/agentic-code-reviewer/internal/domain"
)

type PullRequestKeyV1 struct {
	Host       string `json:"host"`
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Number     int    `json:"number"`
}

func ToPullRequestKeySchema(key domain.PullRequestKey) PullRequestKeyV1 {
	return PullRequestKeyV1{
		Host:       key.Host,
		Owner:      key.Owner,
		Repository: key.Repository,
		Number:     key.Number,
	}
}

func (k PullRequestKeyV1) ToDomain() domain.PullRequestKey {
	return domain.PullRequestKey{
		Host:       k.Host,
		Owner:      k.Owner,
		Repository: k.Repository,
		Number:     k.Number,
	}
}

func (k PullRequestKeyV1) Validate() error {
	return k.ToDomain().Validate()
}

func (k PullRequestKeyV1) String() string {
	return k.ToDomain().String()
}

type RevisionEvidenceV1 struct {
	RequestedBaseRef string `json:"requested_base_ref"`
	ResolvedBaseRef  string `json:"resolved_base_ref"`
	HeadObjectID     string `json:"head_object_id"`
	BaseObjectID     string `json:"base_object_id"`
}

func ToRevisionEvidenceSchema(r domain.RevisionEvidence) RevisionEvidenceV1 {
	return RevisionEvidenceV1{
		RequestedBaseRef: r.RequestedBaseRef,
		ResolvedBaseRef:  r.ResolvedBaseRef,
		HeadObjectID:     r.HeadObjectID,
		BaseObjectID:     r.BaseObjectID,
	}
}

func (r RevisionEvidenceV1) ToDomain() domain.RevisionEvidence {
	return domain.RevisionEvidence{
		RequestedBaseRef: r.RequestedBaseRef,
		ResolvedBaseRef:  r.ResolvedBaseRef,
		HeadObjectID:     r.HeadObjectID,
		BaseObjectID:     r.BaseObjectID,
	}
}

type ReviewTargetV1 struct {
	RepositoryRoot string             `json:"repository_root"`
	WorktreeRoot   string             `json:"worktree_root"`
	Revision       RevisionEvidenceV1 `json:"revision"`
	PullRequest    *PullRequestKeyV1  `json:"pull_request,omitempty"`
}

func ToReviewTargetSchema(t domain.ReviewTarget) ReviewTargetV1 {
	schema := ReviewTargetV1{
		RepositoryRoot: t.RepositoryRoot,
		WorktreeRoot:   t.WorktreeRoot,
		Revision:       ToRevisionEvidenceSchema(t.Revision),
	}
	if t.PullRequest != nil {
		key := ToPullRequestKeySchema(*t.PullRequest)
		schema.PullRequest = &key
	}
	return schema
}

func (t ReviewTargetV1) ToDomain() domain.ReviewTarget {
	target := domain.ReviewTarget{
		RepositoryRoot: t.RepositoryRoot,
		WorktreeRoot:   t.WorktreeRoot,
		Revision:       t.Revision.ToDomain(),
	}
	if t.PullRequest != nil {
		key := t.PullRequest.ToDomain()
		target.PullRequest = &key
	}
	return target
}

func (t ReviewTargetV1) Validate() error {
	return t.ToDomain().Validate()
}

func validateNonEmpty(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}
