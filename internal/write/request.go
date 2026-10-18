package write

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Sriram-PR/ghv/internal/draft"
)

// Request is one write, built from a draft's typed fields. Body is the
// exact bytes sent.
type Request struct {
	Method string
	Path   string
	Body   []byte
}

// SHA256 is the hex SHA-256 of "METHOD PATH\nBODY", the request as the
// preview shows it and the audit log records it.
func (r Request) SHA256() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(r.Method+" "+r.Path+"\n"+string(r.Body))))
}

// The JSON field order is the order the preview shows.
type (
	prCreateBody struct {
		Title               string `json:"title"`
		Head                string `json:"head"`
		Base                string `json:"base"`
		Body                string `json:"body"`
		Draft               bool   `json:"draft"`
		MaintainerCanModify bool   `json:"maintainer_can_modify"`
	}
	prEditBody struct {
		Title *string `json:"title,omitempty"`
		Body  *string `json:"body,omitempty"`
	}
	textBody struct {
		Body string `json:"body"`
	}
	issueBody struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
)

// Build turns a validated draft into its request. fullName is the
// repository's name as GitHub gives it now, which can differ from the
// draft's after a rename or transfer.
func Build(d draft.Draft, fullName string) (Request, error) {
	if err := draft.Validate(d); err != nil {
		return Request{}, err
	}
	owner, name, ok := strings.Cut(fullName, "/")
	if !ok || !segment(owner) || !segment(name) || strings.ContainsAny(fullName, "?#") {
		return Request{}, fmt.Errorf("GitHub's repository name %q is not OWNER/REPO", fullName)
	}
	repo := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name)
	var r Request
	var body any
	switch f := d.Fields.(type) {
	case draft.ForkFields:
		r, body = Request{Method: http.MethodPost, Path: repo + "/forks"}, struct{}{}
	case draft.PRCreateFields:
		r = Request{Method: http.MethodPost, Path: repo + "/pulls"}
		body = prCreateBody{f.Title, f.Head, f.Base, f.Body, f.Draft, f.MaintainerCanModify}
	case draft.PREditFields:
		r = Request{Method: http.MethodPatch, Path: fmt.Sprintf("%s/pulls/%d", repo, f.Number)}
		body = prEditBody{f.Title, f.Body}
	case draft.CommentFields:
		r = Request{Method: http.MethodPost, Path: fmt.Sprintf("%s/issues/%d/comments", repo, f.Number)}
		body = textBody{f.Body}
	case draft.ReviewReplyFields:
		r = Request{Method: http.MethodPost, Path: fmt.Sprintf("%s/pulls/%d/comments/%d/replies", repo, f.Number, f.CommentID)}
		body = textBody{f.Body}
	case draft.IssueCreateFields:
		r = Request{Method: http.MethodPost, Path: repo + "/issues"}
		body = issueBody{f.Title, f.Body}
	default:
		return Request{}, fmt.Errorf("no request for kind %q", d.Kind())
	}
	// GitHub reads <, >, and & as they are; json.Marshal would escape them.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		return Request{}, err
	}
	r.Body = bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	return r, nil
}

func segment(s string) bool { return s != "" && s != "." && s != ".." && !strings.Contains(s, "/") }
