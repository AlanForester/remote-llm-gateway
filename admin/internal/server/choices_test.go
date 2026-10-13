package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelAndPeriodOptions(t *testing.T) {
	models := modelOptions("gpt")
	if !models[3].Selected || models[3].Value != "gpt" {
		t.Fatalf("%+v", models)
	}
	custom := modelOptions("claude-opus")
	last := custom[len(custom)-1]
	if !last.Selected || last.Value != "claude-opus" {
		t.Fatalf("%+v", custom)
	}
	periods := periodOptions("")
	if !periods[0].Selected || periods[0].Value != "day" {
		t.Fatalf("%+v", periods)
	}
	odd := periodOptions("6h")
	if odd[len(odd)-1].Value != "6h" || !odd[len(odd)-1].Selected {
		t.Fatalf("%+v", odd)
	}
}

func TestProbeEchoAndKeyFile(t *testing.T) {
	t.Setenv("GATEWAY_ECHO", "1")
	t.Setenv("OPENAI_API_KEY", "sk-test")
	for _, card := range probeProviders() {
		if card.Ready || card.Detail != "шлюз в режиме эха" {
			t.Fatalf("%+v", card)
		}
	}

	t.Setenv("GATEWAY_ECHO", "0")
	t.Setenv("GROK_COMMAND", filepath.Join(t.TempDir(), "missing-grok"))
	t.Setenv("CLAUDE_COMMAND", filepath.Join(t.TempDir(), "missing-claude"))
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("XAI_API_KEY_FILE", "")
	cards := probeProviders()
	if cards[0].Ready || cards[1].Ready || cards[2].Ready {
		t.Fatalf("%+v", cards)
	}

	key := filepath.Join(t.TempDir(), "xai-key")
	if err := os.WriteFile(key, []byte("xai-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XAI_API_KEY_FILE", key)
	t.Setenv("GROK_COMMAND", os.Args[0])
	grok := probeGrok(false)
	if !grok.Ready {
		t.Fatalf("%+v", grok)
	}
}
