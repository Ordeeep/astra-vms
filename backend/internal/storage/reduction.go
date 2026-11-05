package storage

import (
	"context"
	"errors"
	"time"
)

type Reduction struct {
	Limit     int64 `json:"limit"`
	Usage     int64 `json:"usage"`
	Target    int64 `json:"target"`
	ToFree    int64 `json:"toFree"`
	Protected bool  `json:"protected"`
	Applied   bool  `json:"applied"`
	Deleted   int   `json:"deleted"`
}

func (s *Store) ReductionPreview(tenant string, target int64) (Reduction, error) {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return Reduction{}, err
	}
	limit, exists := s.state.Limits[key(tenant, Recording)]
	if !exists || target < 0 || target > limit {
		return Reduction{}, errors.New("invalid reduction")
	}
	usage := s.usage(tenant, Recording)
	return Reduction{Limit: limit, Usage: usage, Target: target, ToFree: max(0, usage-target)}, nil
}

// The caller quiesces that tenant's capture generation between steps. No
// reader is preempted by this administrative operation. The old limit stays
// authoritative until actual charged allocation fits the new target.
func (s *Store) ReduceRecording(ctx context.Context, tenant string, target int64, expires time.Time) (Reduction, error) {
	s.mu.Lock()
	defer s.unlock()
	if err := s.usable(); err != nil {
		return Reduction{}, err
	}
	limit, exists := s.state.Limits[key(tenant, Recording)]
	if !exists || target < 0 || target > limit {
		return Reduction{}, errors.New("invalid reduction")
	}
	result := Reduction{Limit: limit, Target: target}
	budget := time.Now().Add(100 * time.Millisecond)
	for s.usage(tenant, Recording) > target {
		if ctx.Err() != nil || !time.Now().Before(expires) {
			return result, ErrUnavailable
		}
		if result.Deleted >= 32 || time.Now().After(budget) {
			break
		}
		oldest := s.oldestRecording(tenant, false)
		if oldest == nil {
			result.Protected = true
			break
		}
		if err := s.delete(tenant, oldest.ID, false); err != nil {
			return result, err
		}
		result.Deleted++
	}
	result.Usage = s.usage(tenant, Recording)
	result.ToFree = max(0, result.Usage-target)
	if result.Usage <= target {
		if ctx.Err() != nil || !time.Now().Before(expires) {
			return result, ErrUnavailable
		}
		s.state.Limits[key(tenant, Recording)] = target
		if err := s.commit(ledgerChange{LimitKey: key(tenant, Recording), Limit: target}); err != nil {
			return result, err
		}
		result.Limit = target
		result.Applied = true
	}
	return result, nil
}
