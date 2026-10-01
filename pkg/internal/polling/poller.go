package polling

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// JobStatus represents the status of an async job
type JobStatus string

const (
	// JobStatusQueued indicates the job is queued
	JobStatusQueued JobStatus = "queued"

	// JobStatusProcessing indicates the job is processing
	JobStatusProcessing JobStatus = "processing"

	// JobStatusCompleted indicates the job is completed
	JobStatusCompleted JobStatus = "completed"

	// JobStatusFailed indicates the job failed
	JobStatusFailed JobStatus = "failed"

	// JobStatusCancelled indicates the job was cancelled
	JobStatusCancelled JobStatus = "cancelled"
)

// JobResult represents the result of a job status check
type JobResult struct {
	// Status of the job
	Status JobStatus

	// Output URL (when status is completed)
	OutputURL string

	// Error message (when status is failed)
	Error string

	// Progress percentage (0-100, optional)
	Progress int

	// Additional metadata
	Metadata map[string]interface{}
}

// StatusChecker is a function that checks job status
// It should return JobResult or an error
type StatusChecker func(ctx context.Context) (*JobResult, error)

// PollOptions configures polling behavior
type PollOptions struct {
	// PollIntervalMs is the polling interval in milliseconds (default: 2000ms)
	PollIntervalMs int

	// PollTimeoutMs is the maximum polling time in milliseconds (default: 300000ms = 5 minutes)
	PollTimeoutMs int

	// MaxAttempts is the maximum number of polling attempts (default: unlimited)
	MaxAttempts int

	// BackoffMultiplier for exponential backoff (default: 1.0 = no backoff)
	BackoffMultiplier float64

	// MaxIntervalMs is the maximum interval for exponential backoff (default: 30000ms)
	MaxIntervalMs int
}

// DefaultPollOptions returns default polling options
func DefaultPollOptions() PollOptions {
	return PollOptions{
		PollIntervalMs:    2000,   // 2 seconds
		PollTimeoutMs:     300000, // 5 minutes
		MaxAttempts:       0,      // unlimited
		BackoffMultiplier: 1.0,    // no backoff
		MaxIntervalMs:     30000,  // 30 seconds
	}
}

// PollForCompletion polls a job until completion or timeout
// Returns the JobResult when complete, or an error on timeout/failure
func PollForCompletion(ctx context.Context, checker StatusChecker, opts PollOptions) (*JobResult, error) {
	// Set defaults
	if opts.PollIntervalMs == 0 {
		opts.PollIntervalMs = 2000
	}
	if opts.PollTimeoutMs == 0 {
		opts.PollTimeoutMs = 300000
	}
	if opts.BackoffMultiplier == 0 {
		opts.BackoffMultiplier = 1.0
	}
	if opts.MaxIntervalMs == 0 {
		opts.MaxIntervalMs = 30000
	}

	interval := time.Duration(opts.PollIntervalMs) * time.Millisecond
	timeout := time.Duration(opts.PollTimeoutMs) * time.Millisecond

	// Derive a context bound to the overall poll timeout and cancel it when
	// PollForCompletion returns, so a single in-flight checker call is
	// aborted at the deadline instead of being able to run arbitrarily long
	// past it. Previously PollTimeoutMs was only checked *between* checker
	// calls via a separate timeoutTimer case in the select below, passing
	// the raw caller ctx straight through to checker -- a single
	// slow/hanging status call could block the whole poll loop arbitrarily
	// far past the configured timeout. This mirrors the pollCtx +
	// time.AfterFunc(cancel) pattern the BFL/Fireworks image pollers
	// already use.
	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var timedOut atomic.Bool
	timer := time.AfterFunc(timeout, func() {
		timedOut.Store(true)
		cancel()
	})
	defer timer.Stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	attempts := 0

	for {
		select {
		case <-pollCtx.Done():
			if timedOut.Load() {
				return nil, fmt.Errorf("polling timeout after %v", timeout)
			}
			return nil, ctx.Err()

		case <-ticker.C:
			attempts++

			// Check if max attempts reached
			if opts.MaxAttempts > 0 && attempts > opts.MaxAttempts {
				return nil, fmt.Errorf("max polling attempts (%d) reached", opts.MaxAttempts)
			}

			// Check job status. Pass the deadline-bound pollCtx so this
			// single call cannot outlive the configured timeout.
			result, err := checker(pollCtx)
			if err != nil {
				if timedOut.Load() {
					return nil, fmt.Errorf("polling timeout after %v", timeout)
				}
				if pollCtx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, fmt.Errorf("status check failed: %w", err)
			}

			// Handle different statuses
			switch result.Status {
			case JobStatusCompleted:
				return result, nil

			case JobStatusFailed:
				if result.Error != "" {
					return nil, fmt.Errorf("job failed: %s", result.Error)
				}
				return nil, fmt.Errorf("job failed with unknown error")

			case JobStatusCancelled:
				return nil, fmt.Errorf("job was cancelled")

			case JobStatusProcessing, JobStatusQueued:
				// Continue polling
				// Apply exponential backoff if configured
				if opts.BackoffMultiplier > 1.0 {
					newInterval := time.Duration(float64(interval) * opts.BackoffMultiplier)
					maxInterval := time.Duration(opts.MaxIntervalMs) * time.Millisecond
					if newInterval > maxInterval {
						newInterval = maxInterval
					}
					if newInterval != interval {
						interval = newInterval
						ticker.Reset(interval)
					}
				}
				continue

			default:
				return nil, fmt.Errorf("unknown job status: %s", result.Status)
			}
		}
	}
}

// PollWithProgressCallback polls with a progress callback
// The callback is called on each poll with the current progress
func PollWithProgressCallback(
	ctx context.Context,
	checker StatusChecker,
	opts PollOptions,
	progressCallback func(progress int, status JobStatus),
) (*JobResult, error) {
	// Wrap the checker to call progress callback
	wrappedChecker := func(ctx context.Context) (*JobResult, error) {
		result, err := checker(ctx)
		if err == nil && progressCallback != nil {
			progressCallback(result.Progress, result.Status)
		}
		return result, err
	}

	return PollForCompletion(ctx, wrappedChecker, opts)
}
