package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	twitchDeviceCodeURL = "https://id.twitch.tv/oauth2/device"
	twitchTokenURL      = "https://id.twitch.tv/oauth2/token"
	scopes              = "channel:read:redemptions chat:read chat:edit"
)

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

type deviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// LoadOrAuthorize returns a valid access token. It tries to read a saved token
// from tokenPath first. If missing, it runs the Device Code flow.
func LoadOrAuthorize(clientID, clientSecret, tokenPath string) (string, error) {
	data, err := os.ReadFile(tokenPath)
	if err == nil {
		var tok TokenResponse
		if json.Unmarshal(data, &tok) == nil && tok.AccessToken != "" {
			newTok, err := refreshToken(clientID, clientSecret, tok.RefreshToken)
			if err == nil {
				saveToken(tokenPath, newTok)
				return newTok.AccessToken, nil
			}
			log.Printf("Token refresh failed (%v), re-authorizing...", err)
		}
	}

	tok, err := deviceCodeAuth(clientID, clientSecret)
	if err != nil {
		return "", err
	}
	saveToken(tokenPath, tok)
	return tok.AccessToken, nil
}

func deviceCodeAuth(clientID, clientSecret string) (*TokenResponse, error) {
	// Step 1: Request device code
	resp, err := http.PostForm(twitchDeviceCodeURL, url.Values{
		"client_id": {clientID},
		"scopes":    {scopes},
	})
	if err != nil {
		return nil, fmt.Errorf("request device code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("device code request failed: %d %s", resp.StatusCode, body)
	}

	var dc deviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, fmt.Errorf("decode device code: %w", err)
	}

	// Step 2: Show user the code
	fmt.Println()
	fmt.Println("=== Twitch Authorization ===")
	fmt.Printf("Go to: %s\n", dc.VerificationURI)
	fmt.Printf("Enter code: %s\n", dc.UserCode)
	fmt.Println("Waiting for authorization...")
	fmt.Println()

	// Step 3: Poll for token
	interval := dc.Interval
	if interval < 5 {
		interval = 5
	}
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)

		tok, done, err := pollDeviceToken(clientID, clientSecret, dc.DeviceCode)
		if err != nil {
			return nil, err
		}
		if done {
			return tok, nil
		}
	}

	return nil, fmt.Errorf("authorization timed out")
}

func pollDeviceToken(clientID, clientSecret, deviceCode string) (*TokenResponse, bool, error) {
	resp, err := http.PostForm(twitchTokenURL, url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"device_code":   {deviceCode},
		"grant_type":    {"urn:ietf:params:oauth:grant-type:device_code"},
	})
	if err != nil {
		return nil, false, fmt.Errorf("poll token: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// 400 means "authorization_pending" — keep polling
	if resp.StatusCode == 400 {
		return nil, false, nil
	}

	if resp.StatusCode != 200 {
		return nil, false, fmt.Errorf("token request failed: %d %s", resp.StatusCode, body)
	}

	var tok TokenResponse
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, false, fmt.Errorf("decode token: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, false, fmt.Errorf("empty access token in response")
	}

	return &tok, true, nil
}

func refreshToken(clientID, clientSecret, refresh string) (*TokenResponse, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"refresh_token": {refresh},
		"grant_type":    {"refresh_token"},
	}

	resp, err := http.PostForm(twitchTokenURL, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("refresh returned %d", resp.StatusCode)
	}

	var tok TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return nil, err
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("empty access token")
	}
	return &tok, nil
}

func saveToken(path string, tok *TokenResponse) {
	data, _ := json.Marshal(tok)
	if err := os.WriteFile(path, data, 0600); err != nil {
		log.Printf("warning: could not save token: %v", err)
	}
}

func requiredEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required environment variable %s is not set", key)
	}
	return strings.TrimSpace(v)
}
