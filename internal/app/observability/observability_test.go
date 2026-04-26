package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/health"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestSnapshotComposesScopedSignals(t *testing.T) {
	reader := fakeReader{
		migration: health.MigrationStatus{
			AppliedCount: 30,
			Ready:        true,
		},
		queue: QueueStatus{
			Groups: []QueueGroup{
				{
					Provider: "telegram",
					Status:   "pending",
					Count:    2,
				},
			},
		},
		metrics: AdminMetrics{
			Events:              3,
			Issues:              1,
			NotificationIntents: 2,
		},
	}

	snapshotResult := SnapshotForScope(context.Background(), reader, Scope{})
	snapshot, snapshotErr := snapshotResult.Value()
	if snapshotErr != nil {
		t.Fatalf("snapshot: %v", snapshotErr)
	}

	if snapshot.System.ServiceName != ServiceName {
		t.Fatalf("unexpected service name: %s", snapshot.System.ServiceName)
	}

	if !snapshot.Readiness.Ready || !snapshot.Migration.Ready {
		t.Fatalf("expected ready snapshot: %#v", snapshot)
	}

	if snapshot.Queue.Groups[0].Count != 2 {
		t.Fatalf("unexpected queue: %#v", snapshot.Queue)
	}

	if snapshot.Metrics.Events != 3 {
		t.Fatalf("unexpected metrics: %#v", snapshot.Metrics)
	}
}

func TestReadinessReportsDatabaseFailure(t *testing.T) {
	reader := fakeReader{pingErr: errors.New("down")}

	readiness := Readiness(context.Background(), reader)

	if readiness.Ready {
		t.Fatalf("expected not ready: %#v", readiness)
	}

	if readiness.DatabaseOK {
		t.Fatalf("expected database failure: %#v", readiness)
	}
}

type fakeReader struct {
	pingErr   error
	migration health.MigrationStatus
	queue     QueueStatus
	metrics   AdminMetrics
}

func (reader fakeReader) Ping(ctx context.Context) error {
	return reader.pingErr
}

func (reader fakeReader) MigrationStatus(ctx context.Context) (health.MigrationStatus, error) {
	return reader.migration, nil
}

func (reader fakeReader) QueueStatus(ctx context.Context, scope Scope) result.Result[QueueStatus] {
	return result.Ok(reader.queue)
}

func (reader fakeReader) AdminMetrics(ctx context.Context, scope Scope) result.Result[AdminMetrics] {
	return result.Ok(reader.metrics)
}
