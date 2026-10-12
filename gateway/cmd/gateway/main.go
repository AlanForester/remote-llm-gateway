package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/AlanForester/remote-llm-gateway/gateway/internal/server"
	"github.com/AlanForester/remote-llm-gateway/internal/store"
)

func main() {
	st, err := store.OpenConfigured(env("DATA_FILE", "data/gateway.json"), os.Getenv("REDIS_URL"))
	if err != nil {
		log.Fatal(err)
	}
	srv := server.New(server.Options{
		Store:         st,
		Echo:          os.Getenv("GATEWAY_ECHO") == "1",
		WorkRoot:      env("WORKSPACES_DIR", "data/workspaces"),
		EnvFile:       env("ENV_FILE", ".env"),
		GrokCommand:   fields(os.Getenv("GROK_COMMAND")),
		ClaudeCommand: fields(os.Getenv("CLAUDE_COMMAND")),
		OpenAIBase:    os.Getenv("OPENAI_BASE_URL"),
		OpenAIKey:     os.Getenv("OPENAI_API_KEY"),
		MCPConfig:     os.Getenv("MCP_CONFIG"),
	})
	addr := env("GATEWAY_ADDR", ":8080")
	log.Printf("gateway listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func fields(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}
