package server

import (
	"os"
	"os/exec"
	"strings"
)

type providerCard struct {
	Name   string
	Ready  bool
	Detail string
}

// probeProviders reports whether Grok, Claude, and GPT can be started.
// It looks at the process environment and does not call the providers.
func probeProviders() []providerCard {
	echo := os.Getenv("GATEWAY_ECHO") == "1"
	return []providerCard{
		probeGrok(echo),
		probeClaude(echo),
		probeGPT(echo),
	}
}

func probeGrok(echo bool) providerCard {
	card := providerCard{Name: "Grok"}
	if echo {
		card.Detail = "шлюз в режиме эха"
		return card
	}
	command := firstCommand(os.Getenv("GROK_COMMAND"), "grok")
	if !commandFound(command) {
		card.Detail = "команда не найдена"
		return card
	}
	if !grokKeyReady() {
		card.Detail = "команда есть, ключ не задан"
		return card
	}
	card.Ready = true
	card.Detail = "команда и ключ на месте"
	return card
}

func probeClaude(echo bool) providerCard {
	card := providerCard{Name: "Claude"}
	if echo {
		card.Detail = "шлюз в режиме эха"
		return card
	}
	command := firstCommand(os.Getenv("CLAUDE_COMMAND"), "claude")
	if !commandFound(command) {
		card.Detail = "команда не найдена"
		return card
	}
	card.Ready = true
	card.Detail = "команда на месте"
	return card
}

func probeGPT(echo bool) providerCard {
	card := providerCard{Name: "GPT"}
	if echo {
		card.Detail = "шлюз в режиме эха"
		return card
	}
	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		card.Detail = "ключ OpenAI не задан"
		return card
	}
	card.Ready = true
	card.Detail = "ключ OpenAI задан"
	return card
}

func grokKeyReady() bool {
	path := strings.TrimSpace(os.Getenv("XAI_API_KEY_FILE"))
	if path != "" {
		info, err := os.Stat(path)
		return err == nil && !info.IsDir() && info.Size() > 0
	}
	return strings.TrimSpace(os.Getenv("XAI_API_KEY")) != ""
}

func firstCommand(raw, fallback string) string {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return fallback
	}
	return fields[0]
}

func commandFound(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := exec.LookPath(name)
	return err == nil
}
