package read

import (
	"context"
	"net/http"
)

// GraphQL sends a query-only document. A document that is not plainly a
// query is refused before anything is sent, and GraphQL errors inside a
// 200 response are returned as an *APIError.
func (c *Client) GraphQL(ctx context.Context, query string, variables map[string]any) (*Response, error) {
	resp, err := c.do(ctx, func() (*http.Request, error) {
		req, err := NewGraphQLRequest(ctx, query, variables)
		if err == nil {
			c.setHeaders(req, "")
		}
		return req, err
	})
	if err != nil {
		return nil, err
	}
	if err := graphQLError(resp.Body, resp.Header); err != nil {
		return nil, err
	}
	return resp, nil
}
