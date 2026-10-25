package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/AlanForester/remote-llm-gateway/internal/filesync"
)

func pushProject(gateway, token, sessionID, workspace string) ([]string, error) {
	snap, err := filesync.Collect(workspace)
	if err != nil {
		return nil, err
	}
	chunks := filesync.Chunk(snap.Files, filesync.MaxRequestFiles, filesync.MaxRequestBytes)
	if len(chunks) == 0 {
		chunks = [][]filesync.File{{}}
	}
	if err := putFiles(gateway, token, sessionID, chunks[0]); err != nil {
		return nil, err
	}
	for _, chunk := range chunks[1:] {
		if err := patchFiles(gateway, token, sessionID, chunk); err != nil {
			return nil, err
		}
	}
	return snap.Skipped, nil
}

func pullProject(gateway, token, sessionID, workspace string) ([]filesync.File, error) {
	changed, err := getChanges(gateway, token, sessionID)
	if err != nil {
		return nil, err
	}
	if err := filesync.Apply(workspace, changed); err != nil {
		return nil, err
	}
	return changed, nil
}

func putFiles(gateway, token, sessionID string, files []filesync.File) error {
	return writeFiles(http.MethodPut, gateway, token, sessionID, files)
}

func patchFiles(gateway, token, sessionID string, files []filesync.File) error {
	return writeFiles(http.MethodPatch, gateway, token, sessionID, files)
}

func writeFiles(method, gateway, token, sessionID string, files []filesync.File) error {
	body, err := json.Marshal(map[string]any{"files": files})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(method, sessionURL(gateway, sessionID, "files"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	return statusError(res)
}

func getChanges(gateway, token, sessionID string) ([]filesync.File, error) {
	req, err := http.NewRequest(http.MethodGet, sessionURL(gateway, sessionID, "changes"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if err := statusError(res); err != nil {
		return nil, err
	}
	var payload struct {
		Files []filesync.File `json:"files"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Files == nil {
		payload.Files = []filesync.File{}
	}
	return payload.Files, nil
}

func sessionURL(gateway, sessionID, action string) string {
	return strings.TrimRight(gateway, "/") + "/v1/sessions/" + sessionID + "/" + action
}

func statusError(res *http.Response) error {
	if res.StatusCode < 300 {
		return nil
	}
	raw, _ := io.ReadAll(res.Body)
	var body struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	if body.Message != "" {
		return fmt.Errorf("%s", body.Message)
	}
	if body.Error != "" {
		return fmt.Errorf("%s", body.Error)
	}
	return fmt.Errorf("шлюз ответил %s", res.Status)
}

func describeChange(f filesync.File) string {
	if f.Deleted {
		return "удалён " + f.Path
	}
	return "записан " + f.Path
}
