package collector

import (
	"context"

	"modbus-simulator/internal/tasks"
)

// Options re-exposes the tasks.Options type for external callers.
type Options = tasks.Options

// Run starts the collector with the given options using the internal tasks implementation.
func Run(ctx context.Context, opts Options) error {
	return tasks.InitAndRunCollector(ctx, opts)
}
