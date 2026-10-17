package draft

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	idRE       = regexp.MustCompile(`^d-[0-9a-f]{12}$`)
	ownerRE    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	repoNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	shaRE      = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

func Validate(d Draft) error {
	if owner, name, ok := strings.Cut(d.Repo, "/"); !ok || !ownerRE.MatchString(owner) ||
		!repoNameRE.MatchString(name) || name == "." || name == ".." {
		return invalid("repo %q is not OWNER/REPO", d.Repo)
	}
	if d.CreatedAt.IsZero() {
		return invalid("created_at is missing")
	}
	switch f := d.Fields.(type) {
	case ForkFields:
		return nil
	case PRCreateFields:
		if !validBranch(f.Base) {
			return invalid("base %q is not a valid branch name", f.Base)
		}
		owner, branch, ok := strings.Cut(f.Head, ":")
		if !ok || !ownerRE.MatchString(owner) || !validBranch(branch) {
			return invalid("head %q is not OWNER:BRANCH", f.Head)
		}
		if !shaRE.MatchString(f.HeadSHA) {
			return invalid("head_sha %q is not a commit SHA", f.HeadSHA)
		}
		return checkTitle(f.Title)
	case PREditFields:
		if err := checkNumber(f.Number); err != nil {
			return err
		}
		if f.Title == nil && f.Body == nil {
			return invalid("pr.edit needs a title or a body")
		}
		if f.Title != nil {
			return checkTitle(*f.Title)
		}
		return nil
	case CommentFields:
		return checkNumber(f.Number)
	case ReviewReplyFields:
		if f.CommentID <= 0 {
			return invalid("comment_id must be positive")
		}
		return checkNumber(f.Number)
	case IssueCreateFields:
		return checkTitle(f.Title)
	}
	return invalid("unknown kind")
}

func checkNumber(n int) error {
	if n <= 0 {
		return invalid("number must be positive")
	}
	return nil
}

func checkTitle(t string) error {
	if strings.TrimSpace(t) == "" {
		return invalid("title is empty")
	}
	return nil
}

// validBranch follows git check-ref-format. Non-ASCII is allowed here;
// internal/sanitize flags it in review.
func validBranch(s string) bool {
	if s == "" || len(s) > 255 || s == "@" || !utf8.ValidString(s) {
		return false
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/") || strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") {
		return false
	}
	if strings.Contains(s, "..") || strings.Contains(s, "@{") || strings.Contains(s, "//") {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == 0x7f || (r >= 0x80 && r <= 0x9f) || unicode.IsSpace(r) || strings.ContainsRune(`~^:?*[\`, r) {
			return false
		}
	}
	for _, c := range strings.Split(s, "/") {
		if strings.HasPrefix(c, ".") || strings.HasSuffix(c, ".lock") {
			return false
		}
	}
	return true
}
