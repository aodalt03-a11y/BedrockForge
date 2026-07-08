package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/sandertv/gophertunnel/minecraft/auth"
	"golang.org/x/oauth2"
)

const tokenFile = "token.json"

// authWriter rewrites gophertunnel's device-auth prompt line
// ("Authenticate at <url> using the code <code>.") into structured lines that
// the Android launcher parses: "AUTH_URL <url>" and "AUTH_CODE <code>".
type authWriter struct{}

var authLine = regexp.MustCompile(`Authenticate at (\S+) using the code (\S+)\.`)

func (authWriter) Write(p []byte) (int, error) {
	if m := authLine.FindSubmatch(p); m != nil {
		fmt.Printf("AUTH_URL %s\n", m[1])
		fmt.Printf("AUTH_CODE %s\n", m[2])
		fmt.Printf("Open %s and enter code %s\n", m[1], m[2])
		return len(p), nil
	}
	os.Stdout.Write(p)
	return len(p), nil
}

// runAuth performs the device-code login flow and saves the resulting Live
// Connect token to token.json.
func runAuth() {
	_ = os.Remove(tokenFile)
	tok, err := auth.AndroidConfig.RequestLiveTokenWriter(authWriter{})
	if err != nil {
		log.Fatalf("auth failed: %v", err)
	}
	if err := saveToken(tok); err != nil {
		log.Fatalf("save token: %v", err)
	}
	fmt.Println("token saved")
}

// tokenSource returns a refreshing token source backed by token.json. If no
// token is stored yet, the device-code flow runs first.
func tokenSource() (oauth2.TokenSource, error) {
	tok, err := loadToken()
	if err != nil {
		fmt.Println("No saved login, starting Xbox authentication...")
		tok, err = auth.AndroidConfig.RequestLiveTokenWriter(authWriter{})
		if err != nil {
			return nil, err
		}
		if err := saveToken(tok); err != nil {
			return nil, err
		}
		fmt.Println("token saved")
	}
	return auth.AndroidConfig.RefreshTokenSourceWriter(tok, authWriter{}), nil
}

func loadToken() (*oauth2.Token, error) {
	data, err := os.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}
	tok := new(oauth2.Token)
	if err := json.Unmarshal(data, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func saveToken(tok *oauth2.Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	return os.WriteFile(tokenFile, data, 0o600)
}

// persistToken saves the (possibly refreshed) token after a successful dial so
// the refresh token stays fresh across restarts.
func persistToken(src oauth2.TokenSource) {
	tok, err := src.Token()
	if err != nil {
		return
	}
	_ = saveToken(tok)
}
