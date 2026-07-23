package github

import "testing"

func TestParseRemoteURL_HTTPS(t *testing.T) {
	host, owner, repo, ok := ParseRemoteURL("https://github.com/owner/repo.git")
	if !ok || host != "github.com" || owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected result: host=%q owner=%q repo=%q ok=%v", host, owner, repo, ok)
	}
}

func TestParseRemoteURL_HTTPSWithoutGitSuffix(t *testing.T) {
	host, owner, repo, ok := ParseRemoteURL("https://github.com/owner/repo")
	if !ok || host != "github.com" || owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected result: host=%q owner=%q repo=%q ok=%v", host, owner, repo, ok)
	}
}

func TestParseRemoteURL_SSHShorthand(t *testing.T) {
	host, owner, repo, ok := ParseRemoteURL("git@github.com:owner/repo.git")
	if !ok || host != "github.com" || owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected result: host=%q owner=%q repo=%q ok=%v", host, owner, repo, ok)
	}
}

func TestParseRemoteURL_SSHURLFormat(t *testing.T) {
	host, owner, repo, ok := ParseRemoteURL("ssh://git@github.com/owner/repo.git")
	if !ok || host != "github.com" || owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected result: host=%q owner=%q repo=%q ok=%v", host, owner, repo, ok)
	}
}

func TestParseRemoteURL_SSHURLWithNonDefaultPort(t *testing.T) {
	host, owner, repo, ok := ParseRemoteURL("ssh://git@github.example.com:2222/owner/repo.git")
	if !ok || host != "github.example.com:2222" || owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected result: host=%q owner=%q repo=%q ok=%v", host, owner, repo, ok)
	}
}

func TestParseRemoteURL_CaseInsensitive(t *testing.T) {
	host, owner, repo, ok := ParseRemoteURL("https://GitHub.com/OWNER/REPO")
	if !ok || host != "github.com" || owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected result: host=%q owner=%q repo=%q ok=%v", host, owner, repo, ok)
	}
}

func TestParseRemoteURL_StripsUppercaseGitSuffix(t *testing.T) {
	host, owner, repo, ok := ParseRemoteURL("https://GitHub.com/OWNER/REPO.GIT")
	if !ok || host != "github.com" || owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected result: host=%q owner=%q repo=%q ok=%v", host, owner, repo, ok)
	}
}

func TestParseRemoteURL_StripsMixedCaseGitSuffix(t *testing.T) {
	host, owner, repo, ok := ParseRemoteURL("git@github.com:owner/repo.Git")
	if !ok || host != "github.com" || owner != "owner" || repo != "repo" {
		t.Fatalf("unexpected result: host=%q owner=%q repo=%q ok=%v", host, owner, repo, ok)
	}
}

func TestParseRemoteURL_RejectsHostless(t *testing.T) {
	if _, _, _, ok := ParseRemoteURL("owner/repo"); ok {
		t.Fatal("expected hostless path to be rejected")
	}
}

func TestParseRemoteURL_RejectsMissingRepoSegment(t *testing.T) {
	if _, _, _, ok := ParseRemoteURL("https://github.com/owner"); ok {
		t.Fatal("expected a path with only one segment to be rejected")
	}
}

func TestParseRemoteURL_RejectsEmpty(t *testing.T) {
	if _, _, _, ok := ParseRemoteURL(""); ok {
		t.Fatal("expected empty input to be rejected")
	}
}
