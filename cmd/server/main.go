package main

import (
	"log"

	"orderbuddy-ai/backend/internal/app"
	"orderbuddy-ai/backend/internal/config"
)

func main() {
	cfg := config.Load()

	if err := app.Run(cfg); err != nil {
		log.Fatalf("run app: %v", err)
	}
}
