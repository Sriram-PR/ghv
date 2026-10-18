package fetch

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Event is one of a user's public events, with the payload fields
// ghv-apply log --verify compares. GitHub trims PullRequestEvent's payload
// to the number: it has no html_url (checked 2026-10-18).
type Event struct {
	Type string `json:"type"`
	Repo struct {
		Name string `json:"name"`
	} `json:"repo"`
	CreatedAt time.Time `json:"created_at"`
	Payload   struct {
		Action  string `json:"action"`
		Number  int    `json:"number"`
		Comment struct {
			HTMLURL             string `json:"html_url"`
			PullRequestReviewID int64  `json:"pull_request_review_id"`
		} `json:"comment"`
		Issue struct {
			HTMLURL string `json:"html_url"`
		} `json:"issue"`
		Review struct {
			ID      int64  `json:"id"`
			HTMLURL string `json:"html_url"`
		} `json:"review"`
		Forkee struct {
			HTMLURL string `json:"html_url"`
		} `json:"forkee"`
	} `json:"payload"`
}

// GitHub keeps at most 300 public events, from the last 30 days.
const eventPages = 3

// Events lists login's public events, newest first.
func (c *Client) Events(ctx context.Context, login string) ([]Event, error) {
	var all []Event
	for page := 1; page <= eventPages; page++ {
		var evs []Event
		if err := c.GetJSON(ctx, fmt.Sprintf("/users/%s/events/public?page=%d&per_page=100", url.PathEscape(login), page), &evs); err != nil {
			return nil, err
		}
		all = append(all, evs...)
		if len(evs) < 100 {
			break
		}
	}
	return all, nil
}
