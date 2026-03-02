package main

import (
	"context"
	"os"

	"agent-issue-tracker/internal/ait"
)

func main() {
	ctx := context.Background()

	app, err := ait.Open(ctx)
	if err != nil {
		ait.ExitWithError(ait.NormalizeError(err))
	}
	defer app.Close()

	if err := app.Run(ctx, os.Args[1:]); err != nil {
		ait.ExitWithError(ait.NormalizeError(err))
	}
}
