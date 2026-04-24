package issues

import "testing"

func TestCanTransitionIssueStatus(t *testing.T) {
	cases := []struct {
		name    string
		from    IssueStatus
		to      IssueStatus
		allowed bool
	}{
		{
			name:    "unresolved to resolved",
			from:    IssueStatusUnresolved,
			to:      IssueStatusResolved,
			allowed: true,
		},
		{
			name:    "unresolved to ignored",
			from:    IssueStatusUnresolved,
			to:      IssueStatusIgnored,
			allowed: true,
		},
		{
			name:    "resolved to unresolved",
			from:    IssueStatusResolved,
			to:      IssueStatusUnresolved,
			allowed: true,
		},
		{
			name:    "ignored to unresolved",
			from:    IssueStatusIgnored,
			to:      IssueStatusUnresolved,
			allowed: true,
		},
		{
			name:    "resolved to ignored rejected",
			from:    IssueStatusResolved,
			to:      IssueStatusIgnored,
			allowed: false,
		},
		{
			name:    "same status rejected",
			from:    IssueStatusUnresolved,
			to:      IssueStatusUnresolved,
			allowed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if CanTransitionIssueStatus(tc.from, tc.to) != tc.allowed {
				t.Fatal("unexpected transition result")
			}
		})
	}
}
