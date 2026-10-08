package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/luank3/flared"
)

func main() {
	// Start a local HTTP server
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Welcome to the Go HTTP Server (flared)!")
	})
	go func() {
		log.Println("Server is running at http://localhost:8081")
		if err := http.ListenAndServe(":8081", nil); err != nil {
			log.Fatal(err)
		}
	}()

	// Start Cloudflare Tunnel
	// Because Name and Domain are empty, it will start a Quick Tunnel on trycloudflare.com
	opts := flared.Options{
		OriginURL: "http://localhost:8081",
		ShowLog:   false, // Set to true to see internal cloudflared logs
	}

	tunnel, err := flared.Start(context.Background(), opts)
	if err != nil {
		log.Fatalf("Error creating tunnel: %v", err)
	}
	defer tunnel.Close()

	fmt.Printf("Tunnel URL: %s\n", tunnel.URL())

	// Wait for the tunnel to exit
	if err := tunnel.Wait(); err != nil {
		log.Printf("Tunnel exited with error: %v", err)
	}
}
