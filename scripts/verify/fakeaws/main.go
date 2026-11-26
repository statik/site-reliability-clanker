// Command fakeaws is the stand-in for AWS of scripts/verify/aws-core-check.sh. Never use it
// for a real control plane. It serves the calls that the core on AWS makes:
//
//   - SSM GetParametersByPath, from a JSON file of parameters, ten to a page;
//   - STS AssumeRole, which it passes on to MinIO, so that a job gets a real session of
//     MinIO with a session token;
//   - ECS RunTask, DescribeTasks and StopTask: RunTask runs the runner image with Docker,
//     with the environment of the container override, as Fargate runs a runner task.
//
// It also takes the webhook of a chat platform at /hooks/<name>. It writes one JSON line for
// each call to the record file, and the job token of each task to the tokens file, so that
// the check can look for a token in the log of the core. The record holds no token.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
)

type options struct {
	addr        string
	parameters  string
	record      string
	tokens      string
	logs        string
	image       string
	network     string
	minio       string
	minioKey    string
	minioSecret string
}

func parseOptions() (options, error) {
	var o options
	flag.StringVar(&o.addr, "addr", "127.0.0.1:7272", "listen address")
	flag.StringVar(&o.parameters, "parameters", "", "JSON file: path → setting name → value")
	flag.StringVar(&o.record, "record", "", "file for one JSON line for each call")
	flag.StringVar(&o.tokens, "tokens", "", "file for the job token of each task")
	flag.StringVar(&o.logs, "logs", "", "directory for the output of each task")
	flag.StringVar(&o.image, "image", "", "the runner image")
	flag.StringVar(&o.network, "network", "", "the Docker network of the tasks and MinIO")
	flag.StringVar(&o.minio, "minio", "", "the address of MinIO from this machine")
	flag.StringVar(&o.minioKey, "minio-access-key", "", "the access key of MinIO")
	flag.StringVar(&o.minioSecret, "minio-secret-key", "", "the secret key of MinIO")
	flag.Parse()
	for name, value := range map[string]string{"parameters": o.parameters, "record": o.record,
		"tokens": o.tokens, "logs": o.logs, "image": o.image, "network": o.network,
		"minio": o.minio, "minio-access-key": o.minioKey, "minio-secret-key": o.minioSecret} {
		if value == "" {
			return o, fmt.Errorf("the flag --%s is required", name)
		}
	}
	return o, nil
}

// recorder appends the record of each call to a file.
type recorder struct {
	mu   sync.Mutex
	file *os.File
}

func (r *recorder) add(service, action string, req *http.Request, detail any) {
	line, err := json.Marshal(map[string]any{"service": service, "action": action,
		"access_key": accessKey(req), "detail": detail})
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakeaws: record:", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.file.Write(append(line, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "fakeaws: record:", err)
	}
}

// accessKey returns the access key that signed a request of an AWS SDK.
func accessKey(req *http.Request) string {
	_, credential, found := strings.Cut(req.Header.Get("Authorization"), "Credential=")
	if !found {
		return ""
	}
	key, _, _ := strings.Cut(credential, "/")
	return key
}

// awsError answers a call of the JSON protocol with an error of AWS.
func awsError(w http.ResponseWriter, kind string, err error) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"__type": kind, "message": err.Error()})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, "fakeaws: answer:", err)
	}
}

type server struct {
	opts       options
	record     *recorder
	parameters map[string]map[string]string
	sts        *stsForward
	ecs        *ecsTasks
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if hook, found := strings.CutPrefix(r.URL.Path, "/hooks/"); found {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		s.record.add("webhook", hook, r, map[string]any{"body": string(body)})
		w.WriteHeader(http.StatusOK)
		return
	}
	target := r.Header.Get("X-Amz-Target")
	service, action, _ := strings.Cut(target, ".")
	switch {
	case service == "AmazonSSM" && action == "GetParametersByPath":
		s.getParametersByPath(w, r)
	case service == "AmazonEC2ContainerServiceV20141113":
		s.ecs.serve(w, r, action)
	case target == "" && r.Method == http.MethodPost:
		s.sts.serve(w, r)
	default:
		s.record.add("unknown", target, r, map[string]any{"path": r.URL.Path})
		awsError(w, "UnknownOperationException", fmt.Errorf("the stand-in does not serve %q",
			target))
	}
}

const pageSize = 10

// getParametersByPath answers with the parameters below a path, ten to a page, sorted by
// name. The token of the next page is the index of its first parameter.
func (s *server) getParametersByPath(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path           string
		WithDecryption bool
		NextToken      string
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		awsError(w, "ValidationException", err)
		return
	}
	names := slices.Sorted(maps.Keys(s.parameters[in.Path]))
	start := 0
	if in.NextToken != "" {
		if _, err := fmt.Sscan(in.NextToken, &start); err != nil || start > len(names) {
			awsError(w, "InvalidNextToken", fmt.Errorf("next token %q", in.NextToken))
			return
		}
	}
	end := min(start+pageSize, len(names))
	page := []map[string]any{}
	for _, name := range names[start:end] {
		page = append(page, map[string]any{"Name": in.Path + "/" + name,
			"Value": s.parameters[in.Path][name], "Type": "SecureString", "Version": 1})
	}
	out := map[string]any{"Parameters": page}
	if end < len(names) {
		out["NextToken"] = fmt.Sprint(end)
	}
	s.record.add("ssm", "GetParametersByPath", r, map[string]any{"path": in.Path,
		"with_decryption": in.WithDecryption, "next_token": in.NextToken,
		"names": names[start:end]})
	writeJSON(w, out)
}

func run() error {
	opts, err := parseOptions()
	if err != nil {
		return err
	}
	content, err := os.ReadFile(opts.parameters)
	if err != nil {
		return err
	}
	s := &server{opts: opts}
	if err := json.Unmarshal(content, &s.parameters); err != nil {
		return fmt.Errorf("read %s: %w", opts.parameters, err)
	}
	recordFile, err := os.OpenFile(opts.record, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer recordFile.Close()
	tokenFile, err := os.OpenFile(opts.tokens, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer tokenFile.Close()
	s.record = &recorder{file: recordFile}
	s.sts = newSTSForward(opts, s.record)
	s.ecs = &ecsTasks{opts: opts, record: s.record, tokens: tokenFile, tasks: map[string]*task{}}
	// The check stops the stand-in with SIGTERM. It then removes the containers of the tasks
	// that still run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	defer s.ecs.stopAll()
	httpServer := &http.Server{Addr: opts.addr, Handler: s, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = httpServer.Close()
	}()
	if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fakeaws:", err)
		os.Exit(1)
	}
}
