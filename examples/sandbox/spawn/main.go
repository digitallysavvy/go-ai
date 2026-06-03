package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/ai"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	process, err := ai.NewShellSandbox().Spawn(ctx, ai.SandboxSpawnOptions{
		Command: "printf 'ready\\n'; sleep 1; printf 'done\\n'",
	})
	if err != nil {
		log.Fatal(err)
	}

	stdout, err := io.ReadAll(process.Stdout())
	if err != nil {
		log.Fatal(err)
	}
	result, err := process.Wait()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("exit=%d\n%s", result.ExitCode, stdout)
}
