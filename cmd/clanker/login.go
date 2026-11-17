package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// savedToken is the token of this machine for one control plane.
type savedToken struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

// credentialsPath is the file with the tokens of this user. Only the user can read it.
func credentialsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the configuration directory for the tokens: %w", err)
	}
	return filepath.Join(dir, "clanker", "credentials.json"), nil
}

func readCredentials() (map[string]savedToken, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]savedToken{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	saved := map[string]savedToken{}
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, fmt.Errorf("%s is not valid: %w; remove it and run clanker login again", path, err)
	}
	return saved, nil
}

func writeCredentials(saved map[string]savedToken) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	raw, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the tokens: %w", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// serverKey brings the address of a control plane to the form scheme://host, which names
// its token in the file.
func serverKey(address string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", fmt.Errorf("%q is not the address of a control plane: give it as "+
			"https://host", address)
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func postForm(ctx context.Context, address string, form url.Values, into any) (int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address,
		strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("build the request to %s: %w", address, err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("ask %s: %w", address, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("read the answer of %s: %w", address, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return response.StatusCode, fmt.Errorf("%s answered %d and no JSON: is this a control "+
			"plane with a sign-in?", address, response.StatusCode)
	}
	return response.StatusCode, nil
}

// waitForToken asks for the token until a person decided on the code or the code ended.
func waitForToken(ctx context.Context, server, device string, interval time.Duration) (
	savedToken, error) {
	for {
		select {
		case <-ctx.Done():
			return savedToken{}, ctx.Err()
		case <-time.After(interval):
		}
		var answer struct {
			Error     string `json:"error"`
			Token     string `json:"access_token"`
			ExpiresIn int    `json:"expires_in"`
		}
		if _, err := postForm(ctx, server+"/auth/device/token", url.Values{"device_code": {device}},
			&answer); err != nil {
			return savedToken{}, err
		}
		switch answer.Error {
		case "authorization_pending":
			continue
		case "access_denied":
			return savedToken{}, errors.New("the code was refused in the dashboard")
		case "":
			return savedToken{Token: answer.Token,
				Expires: time.Now().Add(time.Duration(answer.ExpiresIn) * time.Second)}, nil
		default:
			return savedToken{}, fmt.Errorf("the code ended before a person approved it (%s): "+
				"run clanker login again", answer.Error)
		}
	}
}

// runLogin signs this machine in to a control plane with the device flow: it shows a code,
// and a person who is signed in to the dashboard approves the code in a browser.
func runLogin(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("clanker login", flag.ContinueOnError)
	address := flags.String("server", "", "address of the control plane, for example https://clanker.example.com")
	if err := flags.Parse(args); err != nil {
		return err
	}
	server, err := serverKey(*address)
	if err != nil {
		return err
	}
	var code struct {
		Device   string `json:"device_code"`
		User     string `json:"user_code"`
		Page     string `json:"verification_uri_complete"`
		Interval int    `json:"interval"`
	}
	status, err := postForm(ctx, server+"/auth/device/code", nil, &code)
	if err != nil {
		return err
	}
	if status != http.StatusOK || code.Device == "" {
		return fmt.Errorf("%s gave no code (status %d): is this a control plane with a sign-in?",
			server, status)
	}
	fmt.Printf("Open this page in a browser where you are signed in to the dashboard:\n\n  %s\n\n"+
		"Approve only when the page shows the code %s.\nWaiting for the approval...\n",
		code.Page, code.User)
	token, err := waitForToken(ctx, server, code.Device, time.Duration(max(code.Interval, 1))*time.Second)
	if err != nil {
		return err
	}
	saved, err := readCredentials()
	if err != nil {
		return err
	}
	saved[server] = token
	if err := writeCredentials(saved); err != nil {
		return err
	}
	fmt.Printf("This machine is signed in to %s until %s.\nFor git, run once:\n\n"+
		"  git config --global credential.%s.helper '!clanker git-credential'\n", server,
		token.Expires.Local().Format("15:04 on 2 January"), server)
	return nil
}

// runGitCredential is a credential helper for git. git gives the address on standard input
// and reads the user name and the password. The password is the token from clanker login.
func runGitCredential(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: clanker git-credential get|store|erase (git runs this)")
	}
	request := map[string]string{}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if key, value, found := strings.Cut(scanner.Text(), "="); found {
			request[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read the request of git: %w", err)
	}
	server := request["protocol"] + "://" + request["host"]
	saved, err := readCredentials()
	if err != nil {
		return err
	}
	token, found := saved[server]
	switch args[0] {
	case "get":
		// With no answer, git asks the next helper or the person.
		if found && time.Now().Before(token.Expires) {
			fmt.Printf("username=clanker\npassword=%s\n", token.Token)
		}
	case "erase":
		// git erases a credential that the server refused, so the token is of no use.
		if found {
			delete(saved, server)
			return writeCredentials(saved)
		}
	}
	return nil
}
