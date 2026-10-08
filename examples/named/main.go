package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/lucanhost/flared"
)

func main() {
	// 1. Initialize an HTTP server on port 8082
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Welcome to the Go HTTP Server (flared - Named Tunnel)!")
	})

	go func() {
		log.Println("Server is running at http://localhost:8082")
		if err := http.ListenAndServe(":8082", nil); err != nil {
			log.Fatal(err)
		}
	}()

	// 2. Configure Named Tunnel
	// NOTE: To run this code, you must ensure that you have run `cloudflared tunnel login`
	// and have a valid cert.pem file in the ~/.cloudflared/ directory of the server.
	opts := flared.Options{
		OriginURL: "http://localhost:8082",
		Name:      "my-demo-tunnel",  // Change to your desired tunnel name
		Domain:    "api.example.com", // Change to your actual domain on Cloudflare
		// OverwriteDNS repoints an existing DNS record for Domain. Leave it false unless you
		// really want to move a hostname that already serves traffic somewhere else.
		OverwriteDNS: false,
		ShowLog:      false, // Set to true to see internal logs for debugging
	}

	// 3. Start the Tunnel in-process
	// This process will automatically:
	// - Check for cert.pem (triggers the browser login flow if not found)
	// - Create a new tunnel (if it doesn't exist)
	// - Create a DNS CNAME record pointing your domain to this tunnel
	// - Run the tunnel and wait until it is connected to the edge
	tunnel, err := flared.Start(context.Background(), opts)
	if err != nil {
		log.Fatalf("Error creating Named tunnel: %v", err)
	}
	defer tunnel.Close() // Gracefully shut down tunnel on exit (Ctrl+C)

	fmt.Printf("Named Tunnel is ready and running at: %s\n", tunnel.URL())

	// Wait for the tunnel to close or an error to occur
	if err := tunnel.Wait(); err != nil {
		log.Printf("Tunnel exited with error: %v", err)
	}
}
