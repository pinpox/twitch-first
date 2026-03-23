package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func twitchAPIPost(url, clientID, accessToken string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twitch API %s: %d %s", url, resp.StatusCode, string(respBody))
	}

	return nil
}

// GetBroadcasterID looks up a user ID by login name.
func GetBroadcasterID(clientID, accessToken, login string) (string, error) {
	url := fmt.Sprintf("https://api.twitch.tv/helix/users?login=%s", login)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Client-Id", clientID)
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Data) == 0 {
		return "", fmt.Errorf("user %q not found", login)
	}
	return result.Data[0].ID, nil
}
