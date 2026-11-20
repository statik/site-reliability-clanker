package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/statik/site-reliability-clanker/internal/ollama"
)

const modelUsage = `Usage:
  clanker model status [--addr URL] [--user NAME] [--json]
      Print the settings of the language model of a control plane that runs: the mode, the
      provider, the model of each role, and the state of the local inference.
  clanker model start [--addr URL] [--user NAME] [--json]
      Start the local inference: start the Ollama server when it does not answer, and load
      the model into memory. Admin.
  clanker model stop [--addr URL] [--user NAME] [--json]
      Stop the local inference: unload the model, and stop the Ollama server only when the
      control plane started it. Admin.

  --addr URL    address of the control plane (default http://127.0.0.1:7070)
  --user NAME   user of a control plane with no sign-in, for example an admin of the demo.
                A control plane with a sign-in uses the token of clanker login.
  --json        print the answer of the API as JSON

Each command prints one line for each fact. The exit status is 1 when the control plane
refuses the request or does not answer.
`

// modelRoutes are the method and the path of each command of `clanker model`.
var modelRoutes = map[string][2]string{
	"status": {http.MethodGet, "/api/v1/settings/model"},
	"start":  {http.MethodPost, "/api/v1/settings/model/local/start"},
	"stop":   {http.MethodPost, "/api/v1/settings/model/local/stop"},
}

// modelAnswer is the answer of the routes of the model settings.
type modelAnswer struct {
	Mode     string            `json:"mode"`
	Provider string            `json:"provider"`
	Models   map[string]string `json:"models"`
	BaseURL  string            `json:"base_url"`
	KeySet   bool              `json:"key_set"`
	Local    ollama.State      `json:"local"`
	Done     string            `json:"done"`
	Error    string            `json:"error"`
}

// modelRequest is one call of `clanker model`.
type modelRequest struct {
	command string
	server  string
	user    string
	json    bool
}

func parseModelRequest(args []string) (modelRequest, error) {
	request := modelRequest{command: args[0]}
	flags := flag.NewFlagSet("clanker model "+request.command, flag.ContinueOnError)
	address := flags.String("addr", "http://127.0.0.1:7070", "address of the control plane")
	flags.StringVar(&request.user, "user", "", "user of a control plane with no sign-in")
	flags.BoolVar(&request.json, "json", false, "print the answer of the API as JSON")
	if err := flags.Parse(args[1:]); err != nil {
		return modelRequest{}, err
	}
	if flags.NArg() > 0 {
		return modelRequest{}, fmt.Errorf("clanker model %s takes no argument %q\n\n%s",
			request.command, flags.Arg(0), modelUsage)
	}
	if !strings.Contains(*address, "://") {
		*address = "http://" + *address
	}
	var err error
	request.server, err = serverKey(*address)
	return request, err
}

// runModel runs `clanker model`. It calls the API of a control plane that runs, asks no
// question, and prints plain text, so a program can use it as a person does.
func runModel(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("clanker model needs a command\n\n%s", modelUsage)
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, modelUsage)
		return nil
	}
	if _, found := modelRoutes[args[0]]; !found {
		return fmt.Errorf("clanker model %s does not exist\n\n%s", args[0], modelUsage)
	}
	request, err := parseModelRequest(args)
	if err != nil {
		return err
	}
	status, raw, err := callModelRoute(ctx, request)
	if err != nil {
		fmt.Fprintf(out, "FAIL  %v\n", err)
		return errors.New("the control plane did not answer")
	}
	return printModelAnswer(out, request, status, raw)
}

// printModelAnswer prints the answer of the control plane: the JSON as it came, or one
// line for each fact. An answer that is not a success is an error.
func printModelAnswer(out io.Writer, request modelRequest, status int, raw []byte) error {
	var answer modelAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		fmt.Fprintf(out, "FAIL  %s answered %d and no JSON: is this a control plane?\n",
			request.server, status)
		return errors.New("the answer is not from a control plane")
	}
	lines := []string{}
	for _, fact := range modelFacts(answer) {
		lines = append(lines, "ok    "+fact)
	}
	if status != http.StatusOK {
		lines = []string{"FAIL  " + refusalText(status, answer.Error, request)}
	}
	if request.json {
		lines = []string{string(bytes.TrimSpace(raw))}
	}
	fmt.Fprintln(out, strings.Join(lines, "\n"))
	if status != http.StatusOK {
		return fmt.Errorf("the control plane refused the request with status %d", status)
	}
	return nil
}

// refusalText is the error of the control plane, with what to do when the demo did not
// get the name of an admin.
func refusalText(status int, message string, request modelRequest) string {
	if status == http.StatusForbidden && request.command != "status" {
		message += ". On a control plane with no sign-in, give an admin with --user NAME"
	}
	return message
}

// callModelRoute sends the request of a command. A control plane with a sign-in gets the
// token of `clanker login`. A control plane with no sign-in reads the user header.
func callModelRoute(ctx context.Context, request modelRequest) (int, []byte, error) {
	route := modelRoutes[request.command]
	// A start waits until Ollama has read the model into memory.
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	httpRequest, err := http.NewRequestWithContext(ctx, route[0], request.server+route[1], nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build the request to %s: %w", request.server, err)
	}
	token, valid, err := validToken(request.server)
	if err != nil {
		return 0, nil, err
	}
	if valid {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
	}
	if request.user != "" {
		httpRequest.Header.Set("X-Clanker-User", request.user)
	}
	response, err := http.DefaultClient.Do(httpRequest)
	if err != nil {
		return 0, nil, fmt.Errorf("ask the control plane at %s: %w. Start it with clanker "+
			"up, or give its address with --addr", request.server, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("read the answer of %s: %w", request.server, err)
	}
	return response.StatusCode, raw, nil
}

// modelFacts returns one line for each fact of the settings.
func modelFacts(answer modelAnswer) []string {
	lines := []string{"mode: " + answer.Mode, "provider: " + answer.Provider}
	for _, role := range slices.Sorted(maps.Keys(answer.Models)) {
		lines = append(lines, fmt.Sprintf("model of the role %s: %s", role, answer.Models[role]))
	}
	if answer.BaseURL != "" {
		lines = append(lines, "endpoint: "+answer.BaseURL)
	}
	key := "not set"
	if answer.KeySet {
		key = "set"
	}
	lines = append(lines, "key: "+key)
	lines = append(lines, localFacts(answer.Local)...)
	if answer.Done != "" {
		lines = append(lines, "done: "+answer.Done)
	}
	return lines
}

// localFacts returns the lines of the local inference.
func localFacts(local ollama.State) []string {
	if !local.Available {
		return []string{"local inference: not available: " + local.Reason}
	}
	server := "does not answer at " + local.URL
	if local.Running {
		server = fmt.Sprintf("runs at %s, version %s", local.URL, local.Version)
	}
	if local.StartedBySRC {
		server += ", started by S.R.C."
	}
	model := "not known while the server does not answer"
	switch {
	case local.ModelLoaded:
		model = "in memory"
	case local.ModelPresent:
		model = "on the server, not in memory"
	case local.Running:
		model = "not on the server"
	}
	return []string{"local inference: available", "server: " + server,
		fmt.Sprintf("model %s: %s", local.Model, model)}
}
