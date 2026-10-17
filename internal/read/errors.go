package read

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrorCode is the stable `code` in `--json` error output.
type ErrorCode string

const (
	CodeTokenInvalid   ErrorCode = "token_invalid"
	CodeTokenRequired  ErrorCode = "token_required"
	CodeRateLimited    ErrorCode = "rate_limited"
	CodeSecondaryLimit ErrorCode = "secondary_rate_limit"
	CodeClassicBlocked ErrorCode = "classic_token_blocked"
	CodeForbidden      ErrorCode = "forbidden"
	CodeNotFound       ErrorCode = "not_found"
	CodeValidation     ErrorCode = "validation_failed"
	CodeServer         ErrorCode = "server_error"
	CodeHTTP           ErrorCode = "http_error"
	CodeGraphQL        ErrorCode = "graphql_error"
)

const defaultRetryAfter = time.Minute

// APIError is a GitHub error response. Message holds GitHub's text as
// sent; callers sanitise it before printing.
type APIError struct {
	Status     int
	Code       ErrorCode
	Message    string
	Reset      time.Time
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return e.Message }

type githubError struct {
	Message string            `json:"message"`
	Errors  []json.RawMessage `json:"errors"`
}

// details renders GitHub's `errors` array, whose entries are objects or
// plain strings.
func (g githubError) details() string {
	var parts []string
	for _, raw := range g.Errors {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			parts = append(parts, s)
			continue
		}
		var e struct{ Message, Resource, Field, Code string }
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		switch {
		case e.Message != "":
			parts = append(parts, e.Message)
		case e.Resource != "" || e.Field != "":
			parts = append(parts, strings.TrimSpace(e.Resource+"."+e.Field+" "+e.Code))
		}
	}
	return strings.Join(parts, "; ")
}

func errorFor(status int, h http.Header, body []byte, authenticated bool) error {
	if status < 400 {
		return nil
	}
	var g githubError
	json.Unmarshal(body, &g)
	quoted := ""
	if g.Message != "" {
		quoted = " (GitHub: " + g.Message + ")"
	}
	e := &APIError{Status: status}
	switch {
	case status == 401 && authenticated:
		e.Code, e.Message = CodeTokenInvalid, "the read token is invalid or expired"+quoted
	case status == 401:
		e.Code, e.Message = CodeTokenRequired, "this needs a read token"+quoted
	case (status == 403 || status == 429) && h.Get("X-Ratelimit-Remaining") == "0":
		e.Code = CodeRateLimited
		e.Message = "rate limit reached"
		if sec, err := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64); err == nil {
			e.Reset = time.Unix(sec, 0)
			e.Message += "; it resets at " + e.Reset.UTC().Format(time.RFC3339)
		}
	case (status == 403 || status == 429) && (h.Get("Retry-After") != "" || strings.Contains(strings.ToLower(g.Message), "secondary rate limit")):
		e.Code, e.RetryAfter = CodeSecondaryLimit, retryAfter(h)
		e.Message = fmt.Sprintf("secondary rate limit reached; retry after %s%s", e.RetryAfter, quoted)
	case status == 403 && strings.Contains(g.Message, "(classic)"):
		e.Code, e.Message = CodeClassicBlocked, "this organisation may block classic tokens"+quoted
	case status == 403:
		e.Code, e.Message = CodeForbidden, "forbidden"+quoted
	case status == 404:
		e.Code, e.Message = CodeNotFound, "not found, or the token cannot see it"
	case status == 422:
		e.Code, e.Message = CodeValidation, g.Message
		if d := g.details(); d != "" {
			e.Message += ": " + d
		}
	case status >= 500:
		e.Code, e.Message = CodeServer, fmt.Sprintf("GitHub server error (HTTP %d)", status)
	default:
		e.Code, e.Message = CodeHTTP, fmt.Sprintf("HTTP %d%s", status, quoted)
	}
	return e
}

func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if sec, err := strconv.Atoi(v); err == nil && sec >= 0 {
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d.Round(time.Second)
		}
	}
	return defaultRetryAfter
}

// graphQLError returns the errors GitHub reports inside a 200 GraphQL
// response; any data alongside them is discarded.
func graphQLError(body []byte, h http.Header) error {
	var r struct {
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &r) != nil || len(r.Errors) == 0 {
		return nil
	}
	e := &APIError{Status: http.StatusOK, Code: CodeGraphQL}
	var msgs []string
	for _, x := range r.Errors {
		if x.Type == "RATE_LIMITED" {
			e.Code = CodeRateLimited
			if sec, err := strconv.ParseInt(h.Get("X-Ratelimit-Reset"), 10, 64); err == nil {
				e.Reset = time.Unix(sec, 0)
			}
		}
		msgs = append(msgs, x.Message)
	}
	e.Message = "GraphQL: " + strings.Join(msgs, "; ")
	return e
}
