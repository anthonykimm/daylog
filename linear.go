package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	linearAuthorizeURL = "https://linear.app/oauth/authorize"
	linearTokenURL     = "https://api.linear.app/oauth/token"
	linearRedirectURI  = "http://localhost:19284/callback"

	configLinearClientID     = "linear_client_id"
	configLinearClientSecret = "linear_client_secret"
	configLinearAccessToken  = "linear_access_token"
)

type linearTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

// linearHasCredentials checks if client ID and secret are stored.
func linearHasCredentials(db *sql.DB) bool {
	id, _ := getConfig(db, configLinearClientID)
	secret, _ := getConfig(db, configLinearClientSecret)
	return id != "" && secret != ""
}

// linearIsAuthenticated checks if an access token is stored.
func linearIsAuthenticated(db *sql.DB) bool {
	token, _ := getConfig(db, configLinearAccessToken)
	return token != ""
}

// linearGetToken returns the stored access token.
func linearGetToken(db *sql.DB) string {
	token, _ := getConfig(db, configLinearAccessToken)
	return token
}

// linearSaveCredentials stores the client ID and secret.
func linearSaveCredentials(db *sql.DB, clientID, clientSecret string) error {
	if err := setConfig(db, configLinearClientID, clientID); err != nil {
		return err
	}
	return setConfig(db, configLinearClientSecret, clientSecret)
}

// linearClearAll removes all Linear config (credentials and token).
func linearClearAll(db *sql.DB) error {
	for _, key := range []string{configLinearClientID, configLinearClientSecret, configLinearAccessToken} {
		if err := deleteConfig(db, key); err != nil {
			return err
		}
	}
	return nil
}

// linearStartOAuth starts the OAuth flow: opens browser and waits for callback.
// Returns the access token or an error.
func linearStartOAuth(db *sql.DB) (string, error) {
	clientID, _ := getConfig(db, configLinearClientID)
	clientSecret, _ := getConfig(db, configLinearClientSecret)
	if clientID == "" || clientSecret == "" {
		return "", fmt.Errorf("missing Linear client credentials")
	}

	state, err := randomState()
	if err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}

	// Channel to receive the auth code
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errCh <- fmt.Errorf("state mismatch")
			fmt.Fprint(w, "Error: state mismatch. Close this tab and try again.")
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("OAuth error: %s", errMsg)
			fmt.Fprintf(w, "Error: %s. Close this tab.", errMsg)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			fmt.Fprint(w, "Error: no authorization code received. Close this tab.")
			return
		}
		codeCh <- code
		fmt.Fprint(w, "Authorization successful! You can close this tab and return to daylog.")
	})

	server := &http.Server{
		Addr:    ":19284",
		Handler: mux,
	}

	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	// Build authorize URL
	authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&scope=read,write&state=%s&prompt=consent",
		linearAuthorizeURL,
		url.QueryEscape(clientID),
		url.QueryEscape(linearRedirectURI),
		url.QueryEscape(state),
	)

	// Open browser
	if err := openBrowser(authURL); err != nil {
		return "", fmt.Errorf("opening browser: %w", err)
	}

	// Wait for callback (with timeout)
	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return "", err
	case <-time.After(5 * time.Minute):
		return "", fmt.Errorf("OAuth timed out (5 minutes)")
	}

	// Exchange code for token
	token, err := exchangeCode(clientID, clientSecret, code)
	if err != nil {
		return "", err
	}

	// Store token
	if err := setConfig(db, configLinearAccessToken, token); err != nil {
		return "", fmt.Errorf("saving token: %w", err)
	}

	return token, nil
}

func exchangeCode(clientID, clientSecret, code string) (string, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"redirect_uri":  {linearRedirectURI},
		"code":          {code},
	}

	resp, err := http.Post(linearTokenURL, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token exchange failed with status %d", resp.StatusCode)
	}

	var tokenResp linearTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decoding token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}

	return tokenResp.AccessToken, nil
}

func randomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
