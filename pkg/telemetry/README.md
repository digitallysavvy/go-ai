# Telemetry

`pkg/telemetry` provides OpenTelemetry-backed integrations and diagnostic events for Go-AI core operations.

## Step Lifecycle

Step lifecycle telemetry uses `OnStepEnd` as the canonical callback name. `OnStepFinish` remains as a deprecated compatibility path and diagnostic alias for migration.

Implement `OnStepEnd(context.Context, TelemetryStepEndEvent)` to observe completed generation steps.

`OnStepEnd` takes precedence when both new and deprecated callback names are configured, matching the TypeScript June 6 lifecycle rename behavior.

## OpenTelemetry

Register `telemetry.OTelTelemetryIntegration` to emit GenAI semantic convention spans for language model calls, tool execution, and step-level lifecycle events. Step spans are ended by `OnStepEnd`, so aborted or completed generations do not leave spans open.
