package httpadapter

import (
	"net/http"

	observabilityapp "github.com/ivanzakutnii/error-tracker/internal/app/observability"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
)

type observabilitySystemResponse struct {
	ServiceName string `json:"service_name"`
	APIVersion  string `json:"api_version"`
}

type observabilityReadinessResponse struct {
	Ready       bool   `json:"ready"`
	DatabaseOK  bool   `json:"database_ok"`
	SchemaOK    bool   `json:"schema_ok"`
	Description string `json:"description"`
}

type observabilityMigrationResponse struct {
	AppliedCount int  `json:"applied_count"`
	Ready        bool `json:"ready"`
}

type observabilityQueueResponse struct {
	Groups []observabilityQueueGroupResponse `json:"groups"`
}

type observabilityQueueGroupResponse struct {
	Provider            string `json:"provider"`
	Status              string `json:"status"`
	Count               int    `json:"count"`
	OldestNextAttemptAt string `json:"oldest_next_attempt_at,omitempty"`
}

type observabilityMetricsResponse struct {
	Events              int `json:"events"`
	Issues              int `json:"issues"`
	Transactions        int `json:"transactions"`
	UptimeMonitors      int `json:"uptime_monitors"`
	UptimeIncidents     int `json:"uptime_incidents"`
	StatusPages         int `json:"status_pages"`
	NotificationIntents int `json:"notification_intents"`
}

type observabilitySnapshotResponse struct {
	System    observabilitySystemResponse    `json:"system"`
	Readiness observabilityReadinessResponse `json:"readiness"`
	Migration observabilityMigrationResponse `json:"migration"`
	Queue     observabilityQueueResponse     `json:"queue"`
	Metrics   observabilityMetricsResponse   `json:"metrics"`
}

func observabilitySnapshotHandler(
	reader observabilityapp.Reader,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireObservabilitySession(w, r, access, sessions)
		if !sessionOK {
			return
		}

		if reader == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "observability_reader_not_configured"})
			return
		}

		snapshotResult := observabilityapp.SnapshotForScope(
			r.Context(),
			reader,
			observabilityScope(session),
		)
		snapshot, snapshotErr := snapshotResult.Value()
		if snapshotErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "observability_unavailable"})
			return
		}

		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, observabilitySnapshotJSON(snapshot))
	}
}

func observabilitySystemHandler(
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, sessionOK := requireObservabilitySession(w, r, access, sessions)
		if !sessionOK {
			return
		}

		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, observabilitySystemJSON(observabilityapp.System()))
	}
}

func observabilityReadinessHandler(
	reader observabilityapp.Reader,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, sessionOK := requireObservabilitySession(w, r, access, sessions)
		if !sessionOK {
			return
		}

		if reader == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "observability_reader_not_configured"})
			return
		}

		readiness := observabilityapp.Readiness(r.Context(), reader)
		status := http.StatusOK
		if !readiness.Ready {
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, status, observabilityReadinessJSON(readiness))
	}
}

func observabilityMigrationHandler(
	reader observabilityapp.Reader,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, sessionOK := requireObservabilitySession(w, r, access, sessions)
		if !sessionOK {
			return
		}

		if reader == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "observability_reader_not_configured"})
			return
		}

		migrationResult := observabilityapp.Migration(r.Context(), reader)
		migration, migrationErr := migrationResult.Value()
		if migrationErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "migration_status_unavailable"})
			return
		}

		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, observabilityMigrationJSON(migration))
	}
}

func observabilityQueueHandler(
	reader observabilityapp.Reader,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireObservabilitySession(w, r, access, sessions)
		if !sessionOK {
			return
		}

		if reader == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "observability_reader_not_configured"})
			return
		}

		queueResult := reader.QueueStatus(r.Context(), observabilityScope(session))
		queue, queueErr := queueResult.Value()
		if queueErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "queue_status_unavailable"})
			return
		}

		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, observabilityQueueJSON(queue))
	}
}

func observabilityMetricsHandler(
	reader observabilityapp.Reader,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireObservabilitySession(w, r, access, sessions)
		if !sessionOK {
			return
		}

		if reader == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "observability_reader_not_configured"})
			return
		}

		metricsResult := reader.AdminMetrics(r.Context(), observabilityScope(session))
		metrics, metricsErr := metricsResult.Value()
		if metricsErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "metrics_unavailable"})
			return
		}

		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, observabilityMetricsJSON(metrics))
	}
}

func requireObservabilitySession(
	w http.ResponseWriter,
	r *http.Request,
	access operators.Access,
	sessions SessionCodec,
) (operators.OperatorSession, bool) {
	session, sessionOK := validSession(r, access, sessions)
	if !sessionOK {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"detail": "unauthorized"})
		return operators.OperatorSession{}, false
	}

	allowedResult := operators.RequirePermission(session, operators.PermissionViewOps)
	allowedSession, allowedErr := allowedResult.Value()
	if allowedErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "forbidden"})
		return operators.OperatorSession{}, false
	}

	return allowedSession, true
}

func observabilityScope(session operators.OperatorSession) observabilityapp.Scope {
	return observabilityapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

func observabilitySnapshotJSON(snapshot observabilityapp.Snapshot) observabilitySnapshotResponse {
	return observabilitySnapshotResponse{
		System:    observabilitySystemJSON(snapshot.System),
		Readiness: observabilityReadinessJSON(snapshot.Readiness),
		Migration: observabilityMigrationJSON(snapshot.Migration),
		Queue:     observabilityQueueJSON(snapshot.Queue),
		Metrics:   observabilityMetricsJSON(snapshot.Metrics),
	}
}

func observabilitySystemJSON(system observabilityapp.SystemInfo) observabilitySystemResponse {
	return observabilitySystemResponse{
		ServiceName: system.ServiceName,
		APIVersion:  system.APIVersion,
	}
}

func observabilityReadinessJSON(readiness observabilityapp.ReadinessView) observabilityReadinessResponse {
	return observabilityReadinessResponse{
		Ready:       readiness.Ready,
		DatabaseOK:  readiness.DatabaseOK,
		SchemaOK:    readiness.SchemaOK,
		Description: readiness.Description,
	}
}

func observabilityMigrationJSON(migration observabilityapp.MigrationView) observabilityMigrationResponse {
	return observabilityMigrationResponse{
		AppliedCount: migration.AppliedCount,
		Ready:        migration.Ready,
	}
}

func observabilityQueueJSON(queue observabilityapp.QueueStatus) observabilityQueueResponse {
	groups := []observabilityQueueGroupResponse{}
	for _, group := range queue.Groups {
		groups = append(groups, observabilityQueueGroupJSON(group))
	}

	return observabilityQueueResponse{Groups: groups}
}

func observabilityQueueGroupJSON(group observabilityapp.QueueGroup) observabilityQueueGroupResponse {
	return observabilityQueueGroupResponse{
		Provider:            group.Provider,
		Status:              group.Status,
		Count:               group.Count,
		OldestNextAttemptAt: group.OldestNextAttemptAt,
	}
}

func observabilityMetricsJSON(metrics observabilityapp.AdminMetrics) observabilityMetricsResponse {
	return observabilityMetricsResponse{
		Events:              metrics.Events,
		Issues:              metrics.Issues,
		Transactions:        metrics.Transactions,
		UptimeMonitors:      metrics.UptimeMonitors,
		UptimeIncidents:     metrics.UptimeIncidents,
		StatusPages:         metrics.StatusPages,
		NotificationIntents: metrics.NotificationIntents,
	}
}
