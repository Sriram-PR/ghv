package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/Sriram-PR/ghv/internal/archive"
	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{
		path: "run logs", args: []string{"ID"},
		flags: []flag{repoFlag, failedFlag, flag{name: "max-size", kind: intFlag, arg: "MIB", help: "stop past this many MiB of logs", def: "256"}},
		help:  "Print a workflow run's logs, one section per job; needs a read token",
		run:   runRunLogs,
	})
}

var failedFlag = flag{name: "failed", kind: boolFlag, help: "only the jobs that failed, timed out, or were cancelled"}

// runID returns positional argument i as a workflow run ID.
func (a *app) runID(i int) (int64, error) { return a.idArg(i, "run ID") }

// maxSize returns --max-size in bytes.
func (a *app) maxSize() (int64, error) {
	mib := a.inv.int("max-size")
	if mib < 1 || mib > 1<<20 {
		return 0, usagef("--max-size must be from 1 to 1048576 MiB")
	}
	return int64(mib) << 20, nil
}

// runLogs is the stable --json schema of ghv run logs. Conclusion is set
// with --failed only.
type runLogs struct {
	Repo  string   `json:"repo"`
	RunID int64    `json:"run_id"`
	Jobs  []jobLog `json:"jobs"`
}

type jobLog struct {
	Name       string `json:"name"`
	Conclusion string `json:"conclusion,omitempty"`
	Log        string `json:"log"`
}

func runRunLogs(ctx context.Context, a *app) error {
	id, err := a.runID(0)
	if err != nil {
		return err
	}
	max, err := a.maxSize()
	if err != nil {
		return err
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	if !c.Authenticated() {
		return &read.APIError{Status: 401, Code: read.CodeTokenRequired, Message: "run logs need a read token"}
	}
	v := runLogs{Repo: repo, RunID: id, Jobs: []jobLog{}}
	if a.inv.bool("failed") {
		v.Jobs, err = failedJobLogs(ctx, c, repo, id, max)
	} else {
		v.Jobs, err = allJobLogs(ctx, c, repo, id, max)
	}
	if errors.Is(err, read.ErrTooLarge) {
		return fmt.Errorf("%w: the logs are over --max-size %d MiB", read.ErrTooLarge, max>>20)
	}
	if err != nil {
		return err
	}
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	if len(v.Jobs) == 0 {
		fmt.Fprintf(a.stdout, "no failed jobs in run %d\n", id)
		return nil
	}
	for _, j := range v.Jobs {
		head := j.Name
		if j.Conclusion != "" {
			head += " (" + j.Conclusion + ")"
		}
		fmt.Fprintf(a.stdout, "== %s ==\n", sanitize.Line(head))
		log := sanitize.Terminal(j.Log)
		if !strings.HasSuffix(log, "\n") {
			log += "\n"
		}
		if _, err := io.WriteString(a.stdout, log); err != nil {
			return err
		}
	}
	return nil
}

// allJobLogs reads the run's log archive, which holds N_<job>.txt per job
// at the top level and per-job extras in subdirectories.
func allJobLogs(ctx context.Context, c *read.Client, repo string, id, max int64) ([]jobLog, error) {
	var buf bytes.Buffer
	if _, err := c.Download(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/logs", repo, id), "", &buf, max); err != nil {
		return nil, err
	}
	top := func(name string) bool { return !strings.Contains(name, "/") && strings.HasSuffix(name, ".txt") }
	files, err := archive.ReadFiles(bytes.NewReader(buf.Bytes()), int64(buf.Len()), top, max)
	if err != nil {
		return nil, err
	}
	type numbered struct {
		n   int
		job jobLog
	}
	var list []numbered
	for _, f := range files {
		name := strings.TrimSuffix(f.Name, ".txt")
		n := -1
		if prefix, rest, ok := strings.Cut(name, "_"); ok {
			if k, err := strconv.Atoi(prefix); err == nil {
				n, name = k, rest
			}
		}
		list = append(list, numbered{n, jobLog{Name: name, Log: strings.TrimPrefix(string(f.Data), "\ufeff")}})
	}
	slices.SortStableFunc(list, func(x, y numbered) int { return cmp.Compare(x.n, y.n) })
	jobs := []jobLog{}
	for _, l := range list {
		jobs = append(jobs, l.job)
	}
	return jobs, nil
}

func failedJobLogs(ctx context.Context, c *read.Client, repo string, id, max int64) ([]jobLog, error) {
	items, err := c.Paginate(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/jobs?filter=latest", repo, id), "", read.DefaultLimit)
	if err != nil {
		return nil, err
	}
	jobs := []jobLog{}
	for _, raw := range items {
		var g struct {
			ID         int64   `json:"id"`
			Name       string  `json:"name"`
			Status     string  `json:"status"`
			Conclusion *string `json:"conclusion"`
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, fmt.Errorf("GitHub's jobs: %v", err)
		}
		if g.Status != "completed" || g.Conclusion == nil {
			continue
		}
		switch *g.Conclusion {
		case "success", "neutral", "skipped":
			continue
		}
		var buf bytes.Buffer
		if _, err := c.Download(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d/logs", repo, g.ID), "", &buf, max); err != nil {
			return nil, err
		}
		max -= int64(buf.Len())
		jobs = append(jobs, jobLog{g.Name, *g.Conclusion, strings.TrimPrefix(buf.String(), "\ufeff")})
	}
	return jobs, nil
}
