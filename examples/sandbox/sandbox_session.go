//go:build ignore

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

func main() {
	ctx := context.Background()
	sandbox := ai.NewShellSandbox()

	run, err := sandbox.Run(ctx, ai.SandboxProcessOptions{
		Command: "printf '%s' \"$GREETING\"",
		Env:     map[string]string{"GREETING": "hello from run"},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(run.Stdout)

	process, err := sandbox.Spawn(ctx, ai.SandboxProcessOptions{
		Command: "printf '%s' \"$GREETING\"",
		Env:     map[string]string{"GREETING": "hello from spawn"},
	})
	if err != nil {
		log.Fatal(err)
	}
	var output strings.Builder
	_, _ = io.Copy(&output, process.Stdout())
	if _, err := process.Wait(); err != nil {
		log.Fatal(err)
	}
	fmt.Println(output.String())
}
