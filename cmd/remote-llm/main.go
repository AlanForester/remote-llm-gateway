// Command remote-llm is the console client of the gateway.
// It prints the model the admin assigned and forwards prompts over the ACP WebSocket.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	gateway := flag.String("gateway", env("GATEWAY_URL", "http://127.0.0.1:8080"), "адрес шлюза")
	token := flag.String("token", os.Getenv("GATEWAY_TOKEN"), "токен пользователя")
	workspace := flag.String("workspace", ".", "путь локального проекта")
	model := flag.String("model", "", "модель, если администратор её не назначил")
	noSync := flag.Bool("no-sync", false, "не отправлять файлы проекта и не записывать правки")
	flag.Parse()
	if strings.TrimSpace(*token) == "" {
		fmt.Fprintln(os.Stderr, "нужен --token или GATEWAY_TOKEN")
		os.Exit(2)
	}

	created, err := openSession(*gateway, *token, *workspace, *model)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if created.Forced {
		fmt.Printf("Модель: %s (назначена в админке)\n", created.Model)
	} else {
		fmt.Printf("Модель: %s\n", created.Model)
	}
	if created.TokenLimit > 0 {
		fmt.Printf("Токены: %d из %d, сброс %s\n", created.TokensUsed, created.TokenLimit, created.ResetAt.Format(time.RFC3339))
	} else {
		fmt.Println("Лимит токенов не задан")
	}
	if !*noSync {
		skipped, err := pushProject(*gateway, *token, created.SessionID, *workspace)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, name := range skipped {
			fmt.Fprintf(os.Stderr, "пропущен %s\n", name)
		}
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+*token)
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(*gateway, created.SessionID), header)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	in := bufio.NewScanner(os.Stdin)
	fmt.Fprintln(os.Stderr, "Введите запрос. Пустая строка завершает сессию.")
	id := 1
	for in.Scan() {
		text := strings.TrimSpace(in.Text())
		if text == "" {
			return
		}
		if !*noSync {
			if _, err := pushProject(*gateway, *token, created.SessionID, *workspace); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		payload, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"method":  "session/prompt",
			"params":  map[string]string{"text": text},
		})
		id++
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		msg, err := readResult(conn, id-1)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(formatReply(msg))
		if *noSync {
			continue
		}
		changed, err := pullProject(*gateway, *token, created.SessionID, *workspace)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, f := range changed {
			fmt.Fprintln(os.Stderr, describeChange(f))
		}
	}
}

type sessionResponse struct {
	SessionID    string    `json:"sessionId"`
	Model        string    `json:"model"`
	Forced       bool      `json:"forced"`
	TokenLimit   int64     `json:"tokenLimit"`
	TokensUsed   int64     `json:"tokensUsed"`
	ResetAt      time.Time `json:"resetAt"`
	WorkspaceDir string    `json:"workspaceDir"`
	Error        string    `json:"error"`
	Message      string    `json:"message"`
}

func openSession(gateway, token, workspace, model string) (sessionResponse, error) {
	body, _ := json.Marshal(map[string]string{"workspace": workspace, "model": model})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(gateway, "/")+"/v1/sessions", bytes.NewReader(body))
	if err != nil {
		return sessionResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return sessionResponse{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var out sessionResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return sessionResponse{}, err
	}
	if res.StatusCode >= 300 {
		if out.Message != "" {
			return out, fmt.Errorf("%s", out.Message)
		}
		if out.Error != "" {
			return out, fmt.Errorf("%s", out.Error)
		}
		return out, fmt.Errorf("шлюз ответил %s", res.Status)
	}
	return out, nil
}

func wsURL(gateway, sessionID string) string {
	base := strings.TrimRight(gateway, "/")
	base = strings.Replace(base, "https://", "wss://", 1)
	base = strings.Replace(base, "http://", "ws://", 1)
	return base + "/ws/" + sessionID
}

func readResult(conn *websocket.Conn, id int) ([]byte, error) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		if messageID(msg) == id {
			return msg, nil
		}
	}
}

func messageID(msg []byte) int {
	var head struct {
		ID *int `json:"id"`
	}
	if err := json.Unmarshal(msg, &head); err != nil || head.ID == nil {
		return -1
	}
	return *head.ID
}

func formatReply(msg []byte) string {
	var body struct {
		Result struct {
			Text string `json:"text"`
		} `json:"result"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(msg, &body); err != nil {
		return string(msg)
	}
	if body.Error.Message != "" {
		return body.Error.Message
	}
	if body.Result.Text != "" {
		return body.Result.Text
	}
	return string(msg)
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
