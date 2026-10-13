package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/AlanForester/remote-llm-gateway/api/internal/server"
	"github.com/AlanForester/remote-llm-gateway/internal/secrets"
	"github.com/AlanForester/remote-llm-gateway/internal/store"
)

func main() {
	if err := secrets.KeepProcClean(); err != nil {
		log.Fatal(err)
	}
	st, err := store.OpenConfigured(env("DATA_FILE", "data/gateway.json"), os.Getenv("REDIS_URL"))
	if err != nil {
		log.Fatal(err)
	}
	addr := env("API_ADDR", ":8082")
	log.Printf("api listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, server.New(server.Options{Store: st})))
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
