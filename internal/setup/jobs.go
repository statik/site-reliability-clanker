package setup

import (
	"context"
	"errors"
	"sync"
)

var errBusy = errors.New("another step is still running: wait until it ends")

// job is one long step of the wizard, such as the deployment. The page polls it.
type job struct {
	mu     sync.Mutex
	name   string
	status string
	failed string
	lines  []string
}

// JobView is what the page sees of a job.
type JobView struct {
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Error  string   `json:"error"`
	Lines  []string `json:"lines"`
}

func (j *job) view() *JobView {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return &JobView{Name: j.name, Status: j.status, Error: j.failed,
		Lines: append([]string{}, j.lines...)}
}

func (j *job) log(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, line)
}

// start runs work in the background as the one current job.
func (s *Server) start(name string, work func(ctx context.Context, log func(string)) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil && s.job.view().Status == "running" {
		return errBusy
	}
	current := &job{name: name, status: "running", lines: []string{}}
	s.job = current
	go func() {
		err := work(s.baseCtx, current.log)
		current.mu.Lock()
		defer current.mu.Unlock()
		if err != nil {
			current.status, current.failed = "failed", err.Error()
			return
		}
		current.status = "done"
	}()
	return nil
}
