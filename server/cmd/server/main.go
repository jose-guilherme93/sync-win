package main

import (
	"log"

	"sync-win/server/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
