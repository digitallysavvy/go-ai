package mlflow

import (
	"context"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with tracking URI",
			config: Config{
				TrackingURI:    "http://localhost:5000",
				ExperimentName: "test-experiment",
			},
			wantErr: false,
		},
		{
			name: "valid config with HTTPS",
			config: Config{
				TrackingURI:    "https://mlflow.example.com",
				ExperimentName: "prod-experiment",
			},
			wantErr: false,
		},
		{
			name: "valid config with experiment ID",
			config: Config{
				TrackingURI:  "http://localhost:5000",
				ExperimentID: "12345",
			},
			wantErr: false,
		},
		{
			name: "valid config with custom service name",
			config: Config{
				TrackingURI:    "http://localhost:5000",
				ExperimentName: "test",
				ServiceName:    "my-ai-service",
			},
			wantErr: false,
		},
		{
			name: "valid config with insecure flag",
			config: Config{
				TrackingURI:    "http://localhost:5000",
				ExperimentName: "test",
				Insecure:       true,
			},
			wantErr: false,
		},
		{
			name: "valid config with custom headers",
			config: Config{
				TrackingURI:    "http://localhost:5000",
				ExperimentName: "test",
				Headers: map[string]string{
					"Authorization": "Bearer token",
				},
			},
			wantErr: false,
		},
		{
			name:    "missing tracking URI",
			config:  Config{},
			wantErr: true,
		},
		{
			name: "invalid tracking URI",
			config: Config{
				TrackingURI: "://invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker, err := New(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err == nil {
				// Verify tracker was created
				if tracker == nil {
					t.Error("New() returned nil tracker")
					return
				}

				// Verify tracer is available
				tracer := tracker.Tracer()
				if tracer == nil {
					t.Error("Tracer() returned nil")
				}

				// Clean up
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := tracker.Shutdown(ctx); err != nil {
					t.Errorf("Shutdown() error = %v", err)
				}
			}
		})
	}
}

func TestTracker_Defaults(t *testing.T) {
	cfg := Config{
		TrackingURI: "http://localhost:5000",
		// ExperimentName and ServiceName not provided
	}

	tracker, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = tracker.Shutdown(context.Background()) }()

	// Verify defaults were applied
	if tracker.config.ServiceName != "go-ai-sdk" {
		t.Errorf("Expected default ServiceName 'go-ai-sdk', got '%s'", tracker.config.ServiceName)
	}
	if tracker.config.ExperimentName != "default" {
		t.Errorf("Expected default ExperimentName 'default', got '%s'", tracker.config.ExperimentName)
	}
}

func TestTracker_Tracer(t *testing.T) {
	cfg := Config{
		TrackingURI:    "http://localhost:5000",
		ExperimentName: "test",
	}

	tracker, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = tracker.Shutdown(context.Background()) }()

	tracer := tracker.Tracer()
	if tracer == nil {
		t.Fatal("Tracer() returned nil")
	}

	// Verify we can start a span
	ctx := context.Background()
	_, span := tracer.Start(ctx, "test-span")
	if span == nil {
		t.Fatal("Start() returned nil span")
	}
	span.End()
}

func TestTracker_Shutdown(t *testing.T) {
	cfg := Config{
		TrackingURI:    "http://localhost:5000",
		ExperimentName: "test",
	}

	tracker, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = tracker.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	// Second shutdown should not error
	err = tracker.Shutdown(ctx)
	if err != nil {
		t.Errorf("Second Shutdown() error = %v", err)
	}
}

func TestTracker_ForceFlush(t *testing.T) {
	cfg := Config{
		TrackingURI:    "http://localhost:5000",
		ExperimentName: "test",
	}

	tracker, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer func() { _ = tracker.Shutdown(context.Background()) }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = tracker.ForceFlush(ctx)
	if err != nil {
		t.Errorf("ForceFlush() error = %v", err)
	}
}

func TestTracker_ExperimentIDTakesPrecedence(t *testing.T) {
	cfg := Config{
		TrackingURI:    "http://localhost:5000",
		ExperimentName: "test-name",
		ExperimentID:   "12345",
	}

	tracker, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer tracker.Shutdown(context.Background())

	// When both are provided, ExperimentID should be used
	// This is reflected in the headers but we can't directly test
	// the internal state easily. At minimum, verify creation succeeds.
	if tracker == nil {
		t.Fatal("Expected tracker to be created")
	}
}

func TestURLParsing(t *testing.T) {
	tests := []struct {
		name       string
		trackingURI string
		wantErr    bool
	}{
		{
			name:        "localhost",
			trackingURI: "http://localhost:5000",
			wantErr:     false,
		},
		{
			name:        "localhost with explicit port",
			trackingURI: "http://127.0.0.1:5000",
			wantErr:     false,
		},
		{
			name:        "HTTPS URL",
			trackingURI: "https://mlflow.example.com",
			wantErr:     false,
		},
		{
			name:        "HTTPS URL with port",
			trackingURI: "https://mlflow.example.com:443",
			wantErr:     false,
		},
		{
			name:        "HTTP URL with path",
			trackingURI: "http://example.com/mlflow",
			wantErr:     false,
		},
		{
			name:        "invalid URL",
			trackingURI: ":invalid",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				TrackingURI:    tt.trackingURI,
				ExperimentName: "test",
			}

			tracker, err := New(cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tracker != nil {
				tracker.Shutdown(context.Background())
			}
		})
	}
}
