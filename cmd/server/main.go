package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"

	"glm5.2proxy/internal/app"
)

//go:embed all:frontend_dist
var frontendAssets embed.FS

// frontendFS returns the embedded frontend filesystem, if available.
// It is exposed so the API server can serve the React panel at "/".
// When the embed is empty (e.g. dev builds without frontend), it returns nil.
func frontendFS() fs.FS {
	sub, err := fs.Sub(frontendAssets, "frontend_dist")
	if err != nil {
		return nil
	}
	// detect empty embed
	entries, err := fs.ReadDir(sub, ".")
	if err != nil || len(entries) == 0 {
		return nil
	}
	return sub
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	service, err := app.NewWithFrontend(frontendFS())
	if err != nil {
		log.Fatal(err)
	}
	if err := service.Run(ctx); err != nil {
		log.Fatal(err)
	}
}
