package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/Sriram-PR/ghv/internal/read"
)

const watchInterval = 2 * time.Minute

func init() {
	register(&command{
		path: "pr checks", args: []string{"N"},
		flags: []flag{repoFlag, {name: "watch", kind: boolFlag, help: "check again every 2 minutes until nothing is pending"}},
		help:  "Show the check runs and commit statuses of a pull request's head commit; exits 0 whatever they report",
		run:   runPRChecks,
	})
}

// checkView is one item of the --json checks list. Kind is check_run or
// status; State is pending, or the check's conclusion or status state.
type checkView struct {
	Kind        string     `json:"kind"`
	Name        string     `json:"name"`
	State       string     `json:"state"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	URL         string     `json:"url"`
}

// checksView is the stable --json schema of ghv pr checks.
type checksView struct {
	Repo    string      `json:"repo"`
	Number  int         `json:"number"`
	HeadSHA string      `json:"head_sha"`
	Passed  int         `json:"passed"`
	Failed  int         `json:"failed"`
	Pending int         `json:"pending"`
	Checks  []checkView `json:"checks"`
}

// bucket sorts failures first: 0 failed, 1 pending, 2 passed.
func bucket(state string) int {
	switch state {
	case "pending":
		return 1
	case "success", "neutral", "skipped":
		return 2
	}
	return 0
}

func runPRChecks(ctx context.Context, a *app) error {
	repo, n, err := a.repoAndNumber()
	if err != nil {
		return err
	}
	for {
		v, err := a.checks(ctx, repo, n)
		if err != nil {
			return err
		}
		if v.Pending == 0 || !a.inv.bool("watch") {
			return a.printChecks(v)
		}
		fmt.Fprintf(a.stderr, "ghv: %s#%d: %d pending; checking again in 2m\n", repo, n, v.Pending)
		if err := a.wait(ctx, watchInterval); err != nil {
			return err
		}
	}
}

func (a *app) checks(ctx context.Context, repo string, n int) (checksView, error) {
	pr, err := a.getPR(ctx, repo, n)
	if err != nil {
		return checksView{}, err
	}
	sha := pr.Head.SHA
	v := checksView{Repo: repo, Number: n, HeadSHA: sha, Checks: []checkView{}}
	c, err := a.read()
	if err != nil {
		return v, err
	}
	runs, err := c.Paginate(ctx, fmt.Sprintf("repos/%s/commits/%s/check-runs", repo, sha), "", read.DefaultLimit)
	if err != nil {
		return v, err
	}
	for _, raw := range runs {
		var g struct {
			Name        string     `json:"name"`
			Status      string     `json:"status"`
			Conclusion  string     `json:"conclusion"`
			StartedAt   *time.Time `json:"started_at"`
			CompletedAt *time.Time `json:"completed_at"`
			HTMLURL     string     `json:"html_url"`
		}
		if err := json.Unmarshal(raw, &g); err != nil {
			return v, fmt.Errorf("GitHub's check runs: %v", err)
		}
		state := g.Conclusion
		if g.Status != "completed" || state == "" {
			state = "pending"
		}
		v.Checks = append(v.Checks, checkView{"check_run", g.Name, state, g.StartedAt, g.CompletedAt, g.HTMLURL})
	}
	// The combined status reads "pending" when no status was ever set, so
	// only its individual statuses count.
	resp, err := c.Get(ctx, fmt.Sprintf("repos/%s/commits/%s/status", repo, sha), "")
	if err != nil {
		return v, err
	}
	var st struct {
		Statuses []struct {
			Context   string    `json:"context"`
			State     string    `json:"state"`
			TargetURL string    `json:"target_url"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(resp.Body, &st); err != nil {
		return v, fmt.Errorf("GitHub's commit status: %v", err)
	}
	for _, s := range st.Statuses {
		v.Checks = append(v.Checks, checkView{Kind: "status", Name: s.Context, State: s.State, URL: s.TargetURL})
	}
	slices.SortStableFunc(v.Checks, func(x, y checkView) int {
		return cmp.Or(cmp.Compare(bucket(x.State), bucket(y.State)), cmp.Compare(x.Name, y.Name))
	})
	for _, ch := range v.Checks {
		switch bucket(ch.State) {
		case 0:
			v.Failed++
		case 1:
			v.Pending++
		default:
			v.Passed++
		}
	}
	return v, nil
}

func (a *app) printChecks(v checksView) error {
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	fmt.Fprintf(a.stdout, "%s#%d at %s: %d passed, %d failed, %d pending\n", v.Repo, v.Number, short(v.HeadSHA), v.Passed, v.Failed, v.Pending)
	if len(v.Checks) == 0 {
		return nil
	}
	fmt.Fprintln(a.stdout)
	rows := make([][]string, len(v.Checks))
	for i, ch := range v.Checks {
		rows[i] = []string{ch.State, ch.Name, ch.URL}
	}
	writeTable(a.stdout, rows)
	return nil
}
