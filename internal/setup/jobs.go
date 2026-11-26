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
	// completed and total are the bytes of a download. total is 0 for other work.
	completed int64
	total     int64
	// steps are the titles of the steps of the installer, and current the index of the
	// step that runs, or that failed.
	steps   []string
	current int
}

// JobView is what the page sees of a job.
type JobView struct {
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Error  string   `json:"error"`
	Lines  []string `json:"lines"`
	// Completed and Total are the bytes of a download. Total is 0 for other work.
	Completed int64 `json:"completed"`
	Total     int64 `json:"total"`
	// Steps are the steps of the installer or of the removal, and Current the index of the
	// one that runs, or that failed.
	Steps   []string `json:"steps"`
	Current int      `json:"current"`
}

func (j *job) view() *JobView {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return &JobView{Name: j.name, Status: j.status, Error: j.failed,
		Lines: append([]string{}, j.lines...), Completed: j.completed, Total: j.total,
		Steps: append([]string{}, j.steps...), Current: j.current}
}

// setProgress records how far the download of the current job is.
func (s *Server) setProgress(completed, total int64) {
	s.mu.Lock()
	current := s.job
	s.mu.Unlock()
	if current == nil {
		return
	}
	current.mu.Lock()
	defer current.mu.Unlock()
	current.completed, current.total = completed, total
}

// report returns the report of the installer for the job: its steps and its lines.
func (j *job) report(steps []string) Report {
	j.mu.Lock()
	j.steps, j.current = steps, -1
	j.mu.Unlock()
	return Report{Step: func(index int, _ string) {
		j.mu.Lock()
		defer j.mu.Unlock()
		j.current = index
	}, Line: j.log}
}

func (j *job) log(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, line)
}

// start runs work in the background as the one current job.
func (s *Server) start(name string, work func(ctx context.Context, log func(string)) error) error {
	return s.startJob(name, func(ctx context.Context, current *job) error {
		return work(ctx, current.log)
	})
}

// startJob runs work in the background as the one current job, with the job itself.
func (s *Server) startJob(name string, work func(ctx context.Context, current *job) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil && s.job.view().Status == "running" {
		return errBusy
	}
	current := &job{name: name, status: "running", lines: []string{}}
	s.job = current
	go func() {
		err := work(s.baseCtx, current)
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
