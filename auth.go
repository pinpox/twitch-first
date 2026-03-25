package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

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
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	deadline := time.After(time.Duration(dc.ExpiresIn) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("interrupted")
		case <-deadline:
			return nil, fmt.Errorf("authorization timed out")
		case <-ticker.C:
			tok, done, err := pollDeviceToken(clientID, clientSecret, dc.DeviceCode)
			if err != nil {
				return nil, err
			}
			if done {
				return tok, nil
			}
		}
	}
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

// TokenManager handles token storage and refresh.
type TokenManager struct {
	clientID     string
	clientSecret string
	tokenPath    string
	current      TokenResponse
}

func NewTokenManager(clientID, clientSecret, tokenPath string) *TokenManager {
	return &TokenManager{
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenPath:    tokenPath,
	}
}

// Init loads or authorizes and returns the initial access token.
func (tm *TokenManager) Init() (string, error) {
	tok, err := LoadOrAuthorize(tm.clientID, tm.clientSecret, tm.tokenPath)
	if err != nil {
		return "", err
	}
	// Read back the saved token to get the refresh token
	data, err := os.ReadFile(tm.tokenPath)
	if err == nil {
		json.Unmarshal(data, &tm.current)
	}
	if tm.current.AccessToken == "" {
		tm.current.AccessToken = tok
	}
	return tm.current.AccessToken, nil
}

// Refresh gets a new access token using the stored refresh token.
func (tm *TokenManager) Refresh() (string, error) {
	if tm.current.RefreshToken == "" {
		return "", fmt.Errorf("no refresh token available")
	}
	newTok, err := refreshToken(tm.clientID, tm.clientSecret, tm.current.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh: %w", err)
	}
	tm.current = *newTok
	saveToken(tm.tokenPath, newTok)
	log.Println("Token refreshed successfully")
	return newTok.AccessToken, nil
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
