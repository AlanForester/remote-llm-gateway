package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/AlanForester/remote-llm-gateway/admin/internal/server"
	"github.com/AlanForester/remote-llm-gateway/internal/store"
)

func main() {
	token := strings.TrimSpace(os.Getenv("ADMIN_TOKEN"))
	if token == "" {
		log.Fatal("ADMIN_TOKEN is required")
	}
	st, err := store.OpenConfigured(env("DATA_FILE", "data/gateway.json"), os.Getenv("REDIS_URL"))
	if err != nil {
		log.Fatal(err)
	}
	srv := server.New(server.Options{
		Store:      st,
		AdminToken: token,
		EnvFile:    env("ENV_FILE", ".env"),
	})
	addr := env("ADMIN_ADDR", ":8081")
	log.Printf("admin listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
