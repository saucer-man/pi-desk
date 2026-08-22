package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"pi-desk/internal/remotehelper"
)

var buildHash = "development"

func main() {
	if len(os.Args) != 2 || os.Args[1] != "serve-stdio" {
		fmt.Fprintln(os.Stderr, "usage: pi-desk-remote-helper serve-stdio")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	server, err := remotehelper.NewServer(os.Stdin, os.Stdout, remotehelper.Config{BuildHash: buildHash})
	if err == nil {
		err = server.Serve(ctx)
	}
	if err == nil || errors.Is(err, context.Canceled) {
		return
	}
	fmt.Fprintf(os.Stderr, "pi-desk remote helper: %v\n", err)
	os.Exit(1)
}
