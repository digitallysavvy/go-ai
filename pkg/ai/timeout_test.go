package ai

import (
	"context"
	"testing"
	"time"
)

func TestTimeoutConfig_CreateTimeoutContext_Total(t *testing.T) {
	t.Parallel()

	total := 100 * time.Millisecond
	tc := &TimeoutConfig{
		Total: &total,
	}

	ctx, cancel := tc.CreateTimeoutContext(context.Background(), "total")
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have a deadline")
	}

	if time.Until(deadline) > total {
		t.Errorf("deadline is too far in the future")
	}
}

func TestTimeoutConfig_CreateTimeoutContext_PerStep(t *testing.T) {
	t.Parallel()

	perStep := 100 * time.Millisecond
	tc := &TimeoutConfig{
		PerStep: &perStep,
	}

	ctx, cancel := tc.CreateTimeoutContext(context.Background(), "step")
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have a deadline")
	}

	if time.Until(deadline) > perStep {
		t.Errorf("deadline is too far in the future")
	}
}

func TestTimeoutConfig_CreateTimeoutContext_PerChunk(t *testing.T) {
	t.Parallel()

	perChunk := 100 * time.Millisecond
	tc := &TimeoutConfig{
		PerChunk: &perChunk,
	}

	ctx, cancel := tc.CreateTimeoutContext(context.Background(), "chunk")
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected context to have a deadline")
	}

	if time.Until(deadline) > perChunk {
		t.Errorf("deadline is too far in the future")
	}
}

func TestTimeoutConfig_CreateTimeoutContext_NoTimeout(t *testing.T) {
	t.Parallel()

	tc := &TimeoutConfig{}

	ctx, cancel := tc.CreateTimeoutContext(context.Background(), "total")
	defer cancel()

	_, ok := ctx.Deadline()
	if ok {
		t.Error("expected no deadline when no timeout is set")
	}
}

func TestTimeoutConfig_CreateTimeoutContext_NilConfig(t *testing.T) {
	t.Parallel()

	var tc *TimeoutConfig = nil

	ctx, cancel := tc.CreateTimeoutContext(context.Background(), "total")
	defer cancel()

	_, ok := ctx.Deadline()
	if ok {
		t.Error("expected no deadline when config is nil")
	}
}

func TestTimeoutConfig_CreateTimeoutContext_WrongType(t *testing.T) {
	t.Parallel()

	total := 100 * time.Millisecond
	tc := &TimeoutConfig{
		Total: &total,
	}

	// Request a "step" timeout but only Total is set
	ctx, cancel := tc.CreateTimeoutContext(context.Background(), "step")
	defer cancel()

	_, ok := ctx.Deadline()
	if ok {
		t.Error("expected no deadline when requesting wrong timeout type")
	}
}

func TestTimeoutConfig_HasTotal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *TimeoutConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   false,
		},
		{
			name:   "empty config",
			config: &TimeoutConfig{},
			want:   false,
		},
		{
			name: "total set",
			config: &TimeoutConfig{
				Total: func() *time.Duration { d := 10 * time.Second; return &d }(),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasTotal(); got != tt.want {
				t.Errorf("HasTotal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimeoutConfig_HasPerStep(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *TimeoutConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   false,
		},
		{
			name:   "empty config",
			config: &TimeoutConfig{},
			want:   false,
		},
		{
			name: "per-step set",
			config: &TimeoutConfig{
				PerStep: func() *time.Duration { d := 10 * time.Second; return &d }(),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasPerStep(); got != tt.want {
				t.Errorf("HasPerStep() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTimeoutConfig_HasPerChunk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *TimeoutConfig
		want   bool
	}{
		{
			name:   "nil config",
			config: nil,
			want:   false,
		},
		{
			name:   "empty config",
			config: &TimeoutConfig{},
			want:   false,
		},
		{
			name: "per-chunk set",
			config: &TimeoutConfig{
				PerChunk: func() *time.Duration { d := 10 * time.Second; return &d }(),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.HasPerChunk(); got != tt.want {
				t.Errorf("HasPerChunk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTimeoutConfig(t *testing.T) {
	t.Parallel()

	total := 30 * time.Second
	perStep := 10 * time.Second
	perChunk := 5 * time.Second

	config := NewTimeoutConfig(&total, &perStep, &perChunk)

	if config.Total == nil || *config.Total != total {
		t.Errorf("Total = %v, want %v", config.Total, total)
	}
	if config.PerStep == nil || *config.PerStep != perStep {
		t.Errorf("PerStep = %v, want %v", config.PerStep, perStep)
	}
	if config.PerChunk == nil || *config.PerChunk != perChunk {
		t.Errorf("PerChunk = %v, want %v", config.PerChunk, perChunk)
	}
}

func TestNewTimeoutConfig_NilValues(t *testing.T) {
	t.Parallel()

	config := NewTimeoutConfig(nil, nil, nil)

	if config.Total != nil {
		t.Error("Total should be nil")
	}
	if config.PerStep != nil {
		t.Error("PerStep should be nil")
	}
	if config.PerChunk != nil {
		t.Error("PerChunk should be nil")
	}
}

func TestTimeoutConfig_WithTotal(t *testing.T) {
	t.Parallel()

	config := &TimeoutConfig{}
	duration := 30 * time.Second

	result := config.WithTotal(duration)

	if result != config {
		t.Error("WithTotal should return the same config instance")
	}
	if config.Total == nil || *config.Total != duration {
		t.Errorf("Total = %v, want %v", config.Total, duration)
	}
}

func TestTimeoutConfig_WithPerStep(t *testing.T) {
	t.Parallel()

	config := &TimeoutConfig{}
	duration := 10 * time.Second

	result := config.WithPerStep(duration)

	if result != config {
		t.Error("WithPerStep should return the same config instance")
	}
	if config.PerStep == nil || *config.PerStep != duration {
		t.Errorf("PerStep = %v, want %v", config.PerStep, duration)
	}
}

func TestTimeoutConfig_WithPerChunk(t *testing.T) {
	t.Parallel()

	config := &TimeoutConfig{}
	duration := 5 * time.Second

	result := config.WithPerChunk(duration)

	if result != config {
		t.Error("WithPerChunk should return the same config instance")
	}
	if config.PerChunk == nil || *config.PerChunk != duration {
		t.Errorf("PerChunk = %v, want %v", config.PerChunk, duration)
	}
}

func TestTimeoutConfig_BuilderPattern(t *testing.T) {
	t.Parallel()

	config := (&TimeoutConfig{}).
		WithTotal(30 * time.Second).
		WithPerStep(10 * time.Second).
		WithPerChunk(5 * time.Second)

	if !config.HasTotal() {
		t.Error("Total should be set")
	}
	if !config.HasPerStep() {
		t.Error("PerStep should be set")
	}
	if !config.HasPerChunk() {
		t.Error("PerChunk should be set")
	}

	if *config.Total != 30*time.Second {
		t.Errorf("Total = %v, want 30s", *config.Total)
	}
	if *config.PerStep != 10*time.Second {
		t.Errorf("PerStep = %v, want 10s", *config.PerStep)
	}
	if *config.PerChunk != 5*time.Second {
		t.Errorf("PerChunk = %v, want 5s", *config.PerChunk)
	}
}

func TestTimeoutConfig_ContextCancellation(t *testing.T) {
	t.Parallel()

	timeout := 50 * time.Millisecond
	tc := &TimeoutConfig{
		Total: &timeout,
	}

	ctx, cancel := tc.CreateTimeoutContext(context.Background(), "total")
	defer cancel()

	// Wait for timeout
	select {
	case <-ctx.Done():
		if ctx.Err() != context.DeadlineExceeded {
			t.Errorf("expected DeadlineExceeded, got %v", ctx.Err())
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("context should have been cancelled due to timeout")
	}
}

func TestTimeoutConfig_ManualCancellation(t *testing.T) {
	t.Parallel()

	timeout := 1 * time.Second // Long timeout
	tc := &TimeoutConfig{
		Total: &timeout,
	}

	ctx, cancel := tc.CreateTimeoutContext(context.Background(), "total")

	// Cancel immediately
	cancel()

	// Check that context is cancelled
	select {
	case <-ctx.Done():
		if ctx.Err() != context.Canceled {
			t.Errorf("expected Canceled, got %v", ctx.Err())
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("context should have been cancelled immediately")
	}
}
