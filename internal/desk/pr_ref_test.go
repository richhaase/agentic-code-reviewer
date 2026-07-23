package desk

import "testing"

func TestParsePullRequestRef(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		wantErr bool
		host    string
		owner   string
		repo    string
		number  int
	}{
		{name: "valid ref defaults to github.com", ref: "richhaase/agentic-code-reviewer#198", host: DefaultPullRequestHost, owner: "richhaase", repo: "agentic-code-reviewer", number: 198},
		{name: "valid ref with explicit enterprise host", ref: "github.example.com/richhaase/agentic-code-reviewer#198", host: "github.example.com", owner: "richhaase", repo: "agentic-code-reviewer", number: 198},
		{name: "missing hash", ref: "richhaase/agentic-code-reviewer", wantErr: true},
		{name: "missing owner", ref: "/agentic-code-reviewer#198", wantErr: true},
		{name: "missing repo", ref: "richhaase/#198", wantErr: true},
		{name: "missing host with prefix form", ref: "/richhaase/agentic-code-reviewer#198", wantErr: true},
		{name: "non numeric number", ref: "richhaase/agentic-code-reviewer#abc", wantErr: true},
		{name: "zero number", ref: "richhaase/agentic-code-reviewer#0", wantErr: true},
		{name: "too many path segments", ref: "extra/github.com/richhaase/agentic-code-reviewer#198", wantErr: true},
		{name: "empty string", ref: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ParsePullRequestRef(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for ref %q, got key %+v", tt.ref, key)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for ref %q: %v", tt.ref, err)
			}
			if key.Host != tt.host || key.Owner != tt.owner || key.Repository != tt.repo || key.Number != tt.number {
				t.Fatalf("unexpected key for ref %q: %+v", tt.ref, key)
			}
		})
	}
}
