package forge

import (
	"database/sql"
	"fmt"
)

// PullRequest is a proposed merge of Branch into Base.
type PullRequest struct {
	ID         int64  `json:"id"`
	Repo       string `json:"repo"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Branch     string `json:"branch"`
	Base       string `json:"base"`
	HeadCommit string `json:"head_commit"`
	BaseCommit string `json:"base_commit"`
	State      string `json:"state"`
	Author     string `json:"author"`
	WorkItem   string `json:"work_item"`
	CreatedAt  string `json:"created_at"`
	MergedAt   string `json:"merged_at"`
}

const prColumns = `id, repo, title, body, branch, base, head_commit, base_commit, state, author,
	work_item, created_at, merged_at`

func scanPR(row interface{ Scan(...any) error }) (PullRequest, error) {
	var p PullRequest
	err := row.Scan(&p.ID, &p.Repo, &p.Title, &p.Body, &p.Branch, &p.Base, &p.HeadCommit,
		&p.BaseCommit, &p.State, &p.Author, &p.WorkItem, &p.CreatedAt, &p.MergedAt)
	return p, err
}

// OpenPull records a pull request for a branch that exists in the repository.
func (f *Forge) OpenPull(p PullRequest, now string) (PullRequest, error) {
	head, err := f.Head(p.Repo, p.Branch)
	if err != nil {
		return PullRequest{}, fmt.Errorf("open pull request: branch %s: %w", p.Branch, err)
	}
	base, err := f.Head(p.Repo, "main")
	if err != nil {
		return PullRequest{}, fmt.Errorf("open pull request: base main: %w", err)
	}
	p.HeadCommit, p.BaseCommit, p.Base, p.State, p.CreatedAt = head, base, "main", "open", now
	res, err := f.db.Exec(`INSERT INTO prs (repo, title, body, branch, base, head_commit,
		base_commit, state, author, work_item, created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		p.Repo, p.Title, p.Body, p.Branch, p.Base, p.HeadCommit, p.BaseCommit, p.State,
		p.Author, p.WorkItem, p.CreatedAt)
	if err != nil {
		return PullRequest{}, fmt.Errorf("insert pull request for %s: %w", p.Branch, err)
	}
	p.ID, _ = res.LastInsertId()
	return p, nil
}

// Pull returns one pull request.
func (f *Forge) Pull(id int64) (PullRequest, error) {
	p, err := scanPR(f.db.QueryRow(`SELECT `+prColumns+` FROM prs WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return PullRequest{}, fmt.Errorf("pull request %d does not exist", id)
	}
	return p, err
}

// Pulls lists pull requests, newest first. An empty repo lists all repositories.
func (f *Forge) Pulls(repo string) ([]PullRequest, error) {
	query := `SELECT ` + prColumns + ` FROM prs`
	args := []any{}
	if repo != "" {
		query += ` WHERE repo = ?`
		args = append(args, repo)
	}
	rows, err := f.db.Query(query+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, fmt.Errorf("list pull requests: %w", err)
	}
	defer rows.Close()
	pulls := []PullRequest{}
	for rows.Next() {
		p, err := scanPR(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pull request: %w", err)
		}
		pulls = append(pulls, p)
	}
	return pulls, rows.Err()
}

// SetPullWorkItem links a pull request to a work item.
func (f *Forge) SetPullWorkItem(id int64, workItem string) error {
	_, err := f.db.Exec(`UPDATE prs SET work_item = ? WHERE id = ?`, workItem, id)
	return err
}

// RefreshPull records the current head of the branch, for a branch that got new commits.
func (f *Forge) RefreshPull(id int64) (PullRequest, error) {
	p, err := f.Pull(id)
	if err != nil {
		return PullRequest{}, err
	}
	head, err := f.Head(p.Repo, p.Branch)
	if err != nil {
		return PullRequest{}, err
	}
	if _, err := f.db.Exec(`UPDATE prs SET head_commit = ? WHERE id = ?`, head, id); err != nil {
		return PullRequest{}, fmt.Errorf("update head of pull request %d: %w", id, err)
	}
	p.HeadCommit = head
	return p, nil
}

// MergePull merges the pull request into main. It refuses when the head commit is not
// wantCommit, so that only the reviewed commit can merge.
func (f *Forge) MergePull(id int64, wantCommit, author, now string) (string, error) {
	p, err := f.Pull(id)
	if err != nil {
		return "", err
	}
	if p.State != "open" {
		return "", fmt.Errorf("merge pull request %d: state is %s, not open", id, p.State)
	}
	head, err := f.Head(p.Repo, p.Branch)
	if err != nil {
		return "", err
	}
	if head != wantCommit {
		return "", fmt.Errorf("merge pull request %d: branch head %s is not the approved commit %s",
			id, head, wantCommit)
	}
	merged, err := f.Merge(p.Repo, p.Branch, author,
		fmt.Sprintf("Merge pull request #%d: %s", p.ID, p.Title))
	if err != nil {
		return "", err
	}
	_, err = f.db.Exec(`UPDATE prs SET state = 'merged', merged_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return "", fmt.Errorf("mark pull request %d merged: %w", id, err)
	}
	return merged, nil
}

// ClosePull closes a pull request without a merge.
func (f *Forge) ClosePull(id int64) error {
	_, err := f.db.Exec(`UPDATE prs SET state = 'closed' WHERE id = ? AND state = 'open'`, id)
	return err
}
