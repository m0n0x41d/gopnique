package tokens

import "testing"

func TestProjectTokenScopeAllows(t *testing.T) {
	cases := []struct {
		name     string
		scope    ProjectTokenScope
		required ProjectTokenScope
		allowed  bool
	}{
		{
			name:     "read allows read",
			scope:    ProjectTokenScopeRead,
			required: ProjectTokenScopeRead,
			allowed:  true,
		},
		{
			name:     "read denies admin",
			scope:    ProjectTokenScopeRead,
			required: ProjectTokenScopeAdmin,
			allowed:  false,
		},
		{
			name:     "admin allows read",
			scope:    ProjectTokenScopeAdmin,
			required: ProjectTokenScopeRead,
			allowed:  true,
		},
		{
			name:     "admin allows admin",
			scope:    ProjectTokenScopeAdmin,
			required: ProjectTokenScopeAdmin,
			allowed:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.scope.Allows(tc.required) != tc.allowed {
				t.Fatal("unexpected scope result")
			}
		})
	}
}

func TestProjectTokenSecretRequiresDedicatedPrefix(t *testing.T) {
	_, err := NewProjectTokenSecret("550e8400e29b41d4a716446655440000")
	if err == nil {
		t.Fatal("expected sentry key shaped secret to fail")
	}

	_, validErr := NewProjectTokenSecret("etp_550e8400e29b41d4a716446655440000")
	if validErr != nil {
		t.Fatalf("expected api token secret: %v", validErr)
	}
}
