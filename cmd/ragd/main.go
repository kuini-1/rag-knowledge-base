// Command ragd runs the knowledge base API and its evaluation tools.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usage = `ragd: a RAG knowledge base with cited answers

Usage:
  ragd serve      [flags]   run the API
  ragd user add   [flags]   create a user (password from RAG_NEW_PASSWORD)
  ragd ablation   [flags]   chunking and retrieval study on the eval corpus (no LLM calls)
  ragd eval       [flags]   answer the eval questions with Claude and grade the answers

Run "ragd <command> -h" for flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "serve":
		err = cmdServe(ctx, args)
	case "user":
		err = cmdUser(ctx, args)
	case "ablation":
		err = cmdAblation(ctx, args)
	case "eval":
		err = cmdEval(ctx, args)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
