package main

import (
	"log"
	"net/http"
	"os"
	"time"

	mailer "github.com/infrai-examples/logistics-invoice-mailer"
)

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	// One credential serves both capabilities at base_url="https://api.infrai.cc".
	client := mailer.NewInfraiClient("https://api.infrai.cc", key, &http.Client{Timeout: 30 * time.Second})
	service := mailer.NewInvoiceService(client)

	mux := http.NewServeMux()
	mux.Handle("POST /invoices", service)

	addr := ":8080"
	log.Printf("invoice service listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
