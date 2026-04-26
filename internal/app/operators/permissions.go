package operators

import (
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Permission string

const (
	PermissionReadProject   Permission = "read_project"
	PermissionTriageIssues  Permission = "triage_issues"
	PermissionManageAlerts  Permission = "manage_alerts"
	PermissionManageMembers Permission = "manage_members"
	PermissionManageTokens  Permission = "manage_tokens"
	PermissionViewAudit     Permission = "view_audit"
	PermissionViewOps       Permission = "view_ops"
)

func RequirePermission(
	session OperatorSession,
	permission Permission,
) result.Result[OperatorSession] {
	if !permission.Valid() {
		return result.Err[OperatorSession](errors.New("permission is invalid"))
	}

	if !Can(session, permission) {
		return result.Err[OperatorSession](errors.New("permission denied"))
	}

	return result.Ok(session)
}

func Can(session OperatorSession, permission Permission) bool {
	if session.OrganizationRole == "owner" {
		return true
	}

	if session.ProjectRole == "owner" || session.ProjectRole == "admin" {
		return permission == PermissionReadProject ||
			permission == PermissionTriageIssues ||
			permission == PermissionManageAlerts ||
			permission == PermissionManageMembers ||
			permission == PermissionManageTokens ||
			permission == PermissionViewAudit ||
			permission == PermissionViewOps
	}

	if session.ProjectRole == "member" {
		return permission == PermissionReadProject
	}

	return false
}

func (permission Permission) Valid() bool {
	return permission == PermissionReadProject ||
		permission == PermissionTriageIssues ||
		permission == PermissionManageAlerts ||
		permission == PermissionManageMembers ||
		permission == PermissionManageTokens ||
		permission == PermissionViewAudit ||
		permission == PermissionViewOps
}
