package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/retention"
)

type RetentionTaskConfig struct {
	BatchSize int
}

type retentionTask struct {
	store retention.Store
	cfg   RetentionTaskConfig
}

func NewRetentionTask(
	store retention.Store,
	cfg RetentionTaskConfig,
) Task {
	return retentionTask{
		store: store,
		cfg:   cfg,
	}
}

func (task retentionTask) RunOnce(ctx context.Context) error {
	summaryResult := retention.Run(
		ctx,
		task.store,
		retention.Command{
			Now:       time.Now().UTC(),
			BatchSize: task.cfg.BatchSize,
		},
	)
	_, summaryErr := summaryResult.Value()

	return summaryErr
}
