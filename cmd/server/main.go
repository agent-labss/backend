package main

import (
	"log"

	"ai/backend/internal/app"
	"ai/backend/internal/config"
)

func main() {
	cfg := config.Load()

	if err := app.Run(cfg); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
