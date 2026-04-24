package domain

import (
	"errors"
	"time"
)

type TimePoint struct {
	value time.Time
}

func NewTimePoint(value time.Time) (TimePoint, error) {
	if value.IsZero() {
		return TimePoint{}, errors.New("time point is required")
	}

	return TimePoint{value: value.UTC()}, nil
}

func (point TimePoint) Time() time.Time {
	return point.value
}
