package operators

import "testing"

func TestPermissionTable(t *testing.T) {
	cases := []struct {
		name       string
		session    OperatorSession
		permission Permission
		allowed    bool
	}{
		{
			name:       "organization owner can manage members",
			session:    OperatorSession{OrganizationRole: "owner", ProjectRole: "member"},
			permission: PermissionManageMembers,
			allowed:    true,
		},
		{
			name:       "project admin can manage alerts",
			session:    OperatorSession{ProjectRole: "admin"},
			permission: PermissionManageAlerts,
			allowed:    true,
		},
		{
			name:       "project member can read",
			session:    OperatorSession{ProjectRole: "member"},
			permission: PermissionReadProject,
			allowed:    true,
		},
		{
			name:       "project admin can triage",
			session:    OperatorSession{ProjectRole: "admin"},
			permission: PermissionTriageIssues,
			allowed:    true,
		},
		{
			name:       "project member cannot triage",
			session:    OperatorSession{ProjectRole: "member"},
			permission: PermissionTriageIssues,
			allowed:    false,
		},
		{
			name:       "project member cannot manage alerts",
			session:    OperatorSession{ProjectRole: "member"},
			permission: PermissionManageAlerts,
			allowed:    false,
		},
		{
			name:       "project member cannot manage tokens",
			session:    OperatorSession{ProjectRole: "member"},
			permission: PermissionManageTokens,
			allowed:    false,
		},
		{
			name:       "project member cannot view audit",
			session:    OperatorSession{ProjectRole: "member"},
			permission: PermissionViewAudit,
			allowed:    false,
		},
		{
			name:       "missing role cannot read",
			session:    OperatorSession{},
			permission: PermissionReadProject,
			allowed:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if Can(tc.session, tc.permission) != tc.allowed {
				t.Fatalf("unexpected permission result")
			}
		})
	}
}
