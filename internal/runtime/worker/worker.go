package worker

import (
	"context"
	"time"
)

type Task interface {
	RunOnce(ctx context.Context) error
}

type Worker struct {
	interval time.Duration
	tasks    []Task
}

func New(tasks ...Task) Worker {
	return Worker{
		interval: time.Second,
		tasks:    append([]Task{}, tasks...),
	}
}

func (worker Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			runErr := worker.runTasks(ctx)
			if runErr != nil {
				return runErr
			}
		}
	}
}

func (worker Worker) runTasks(ctx context.Context) error {
	for _, task := range worker.tasks {
		runErr := task.RunOnce(ctx)
		if runErr != nil {
			return runErr
		}
	}

	return nil
}
