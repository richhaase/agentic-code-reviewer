package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	gitpkg "github.com/richhaase/agentic-code-reviewer/internal/git"
)

type Source interface {
	LoadWithWarnings(context.Context) (*LoadResult, error)
}

const (
	SourceKindDisabled           = "disabled"
	SourceKindDefaults           = "defaults"
	SourceKindRepositoryRevision = "repository-revision"
	SourceKindFilesystem         = "filesystem"
)

type CanonicalPolicy string

const (
	CanonicalRemoteDefault CanonicalPolicy = "remote-default"
	CanonicalNamedBranch   CanonicalPolicy = "named-branch"
)

type TrustedSourceRequest struct {
	RepositoryRoot string
	Remote         string
	Branch         string
	Policy         CanonicalPolicy
	Disabled       bool
}

type SourceIdentity struct {
	Kind          string
	Locator       string
	Ref           string
	Revision      string
	ConfigPresent bool
	ConfigDigest  string
}

type RepositoryRevisionSource struct {
	RepositoryRoot string
	Ref            string
	Revision       string
}

type DefaultsSource struct {
	Reason string
}

type DisabledSource struct {
	Locator string
}

func (s DisabledSource) LoadWithWarnings(_ context.Context) (*LoadResult, error) {
	return &LoadResult{
		Config: &Config{},
		Source: SourceIdentity{
			Kind:    SourceKindDisabled,
			Locator: s.Locator,
		},
	}, nil
}

func (s DefaultsSource) LoadWithWarnings(_ context.Context) (*LoadResult, error) {
	return &LoadResult{
		Config: &Config{},
		Source: SourceIdentity{
			Kind:    SourceKindDefaults,
			Locator: s.Reason,
		},
	}, nil
}

func ResolveTrustedSource(ctx context.Context, request TrustedSourceRequest) (Source, error) {
	if request.Disabled {
		return DisabledSource{Locator: "--no-config"}, nil
	}
	if request.RepositoryRoot == "" {
		return nil, fmt.Errorf("trusted configuration repository root must not be empty")
	}

	switch request.Policy {
	case CanonicalRemoteDefault:
		if request.Remote == "" {
			return nil, fmt.Errorf("remote-default canonical policy requires a remote")
		}
		branch, err := gitpkg.RemoteDefaultBranch(ctx, request.RepositoryRoot, request.Remote)
		if err != nil {
			return nil, fmt.Errorf("failed to select trusted review configuration: %w", err)
		}
		return resolveRemoteBranchSource(ctx, request.RepositoryRoot, request.Remote, branch)
	case CanonicalNamedBranch:
		if request.Branch == "" {
			return nil, fmt.Errorf("named-branch canonical policy requires a branch")
		}
		if request.Remote != "" {
			return resolveRemoteBranchSource(ctx, request.RepositoryRoot, request.Remote, request.Branch)
		}
		ref := "refs/heads/" + request.Branch
		exists, err := gitpkg.RefExists(ctx, request.RepositoryRoot, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect local canonical review configuration: %w", err)
		}
		if !exists {
			return DefaultsSource{Reason: "canonical branch " + request.Branch + " is unavailable"}, nil
		}
		source, err := NewRepositoryRevisionSource(ctx, request.RepositoryRoot, ref)
		if err != nil {
			return nil, fmt.Errorf("failed to snapshot local canonical review configuration: %w", err)
		}
		return source, nil
	default:
		return nil, fmt.Errorf("unsupported canonical policy %q", request.Policy)
	}
}

func resolveRemoteBranchSource(ctx context.Context, repositoryRoot, remote, branch string) (Source, error) {
	ref := trustedSnapshotRef(remote, branch)
	if err := gitpkg.FetchRemoteBranchToRef(ctx, repositoryRoot, remote, branch, ref); err != nil {
		return nil, fmt.Errorf("failed to refresh trusted review configuration: %w", err)
	}
	source, err := NewRepositoryRevisionSource(ctx, repositoryRoot, ref)
	if err != nil {
		return nil, fmt.Errorf("failed to snapshot trusted review configuration: %w", err)
	}
	return source, nil
}

func trustedSnapshotRef(remote, branch string) string {
	return "refs/acr/trusted-config/" + remote + "/" + branch
}

func NewRepositoryRevisionSource(ctx context.Context, repositoryRoot, ref string) (RepositoryRevisionSource, error) {
	revision, err := gitpkg.ResolveCommit(ctx, repositoryRoot, ref)
	if err != nil {
		return RepositoryRevisionSource{}, err
	}
	return RepositoryRevisionSource{
		RepositoryRoot: repositoryRoot,
		Ref:            ref,
		Revision:       revision,
	}, nil
}

func (s RepositoryRevisionSource) LoadWithWarnings(ctx context.Context) (*LoadResult, error) {
	data, err := gitpkg.ReadFileAtCommit(ctx, s.RepositoryRoot, s.Revision, ConfigFileName)
	if errors.Is(err, gitpkg.ErrPathNotFoundAtRevision) {
		return &LoadResult{
			Config: &Config{},
			Source: SourceIdentity{
				Kind:     SourceKindRepositoryRevision,
				Locator:  s.RepositoryRoot,
				Ref:      s.Ref,
				Revision: s.Revision,
			},
			readConfigRelative: s.readConfigRelative,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load trusted configuration: %w", err)
	}

	result, err := loadDataWithWarnings(data)
	if result != nil {
		result.Source = SourceIdentity{
			Kind:          SourceKindRepositoryRevision,
			Locator:       s.RepositoryRoot,
			Ref:           s.Ref,
			Revision:      s.Revision,
			ConfigPresent: true,
			ConfigDigest:  digest(data),
		}
		result.readConfigRelative = s.readConfigRelative
	}
	return result, err
}

func (s RepositoryRevisionSource) readConfigRelative(ctx context.Context, relativePath string) ([]byte, error) {
	data, err := gitpkg.ReadFileAtCommit(ctx, s.RepositoryRoot, s.Revision, relativePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read trusted configuration input %q from %s: %w", relativePath, s.Revision, err)
	}
	return data, nil
}

func loadDataWithWarnings(data []byte) (*LoadResult, error) {
	warnings := checkUnknownKeys(data)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", ConfigFileName, err)
	}

	if err := cfg.validatePatterns(); err != nil {
		return nil, err
	}

	if cfg.ReviewerAgent != nil {
		warnings = append(warnings, `"reviewer_agent" is deprecated, use "reviewer_agents" list instead`)
		if len(cfg.ReviewerAgents) > 0 {
			warnings = append(warnings, `both "reviewer_agent" and "reviewer_agents" are set; "reviewer_agents" takes precedence`)
		}
	}

	result := &LoadResult{Config: &cfg, Warnings: warnings}
	if err := cfg.Validate(); err != nil {
		return result, fmt.Errorf("%s: %w", ConfigFileName, err)
	}
	return result, nil
}

func newFileSystemLoadResult(path string, data []byte, configPresent bool) *LoadResult {
	configDir := filepath.Dir(path)
	identity := SourceIdentity{
		Kind:          SourceKindFilesystem,
		Locator:       path,
		ConfigPresent: configPresent,
	}
	if configPresent {
		identity.ConfigDigest = digest(data)
	}
	return &LoadResult{
		ConfigDir: configDir,
		Source:    identity,
		readConfigRelative: func(_ context.Context, relativePath string) ([]byte, error) {
			resolvedPath := relativePath
			if !filepath.IsAbs(resolvedPath) {
				resolvedPath = filepath.Join(configDir, resolvedPath)
			}
			return os.ReadFile(resolvedPath)
		},
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
