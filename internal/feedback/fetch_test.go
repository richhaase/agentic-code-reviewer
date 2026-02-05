package feedback

import "testing"

func TestPRContext_HasContent(t *testing.T) {
	tests := []struct {
		name     string
		ctx      PRContext
		expected bool
	}{
		{
			name:     "empty context",
			ctx:      PRContext{},
			expected: false,
		},
		{
			name:     "description only",
			ctx:      PRContext{Description: "Fix bug"},
			expected: true,
		},
		{
			name:     "comments only",
			ctx:      PRContext{Comments: []Comment{{Body: "LGTM"}}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ctx.HasContent(); got != tt.expected {
				t.Errorf("HasContent() = %v, want %v", got, tt.expected)
			}
		})
	}
}
