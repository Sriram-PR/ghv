package doctor

import (
	"context"
	"fmt"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func init() { register("queue", queue) }

// queue says where this user's drafts go: an agent running as its own
// user reaches the human's ghv-apply only through the shared queue.
func queue(_ context.Context, e *Env) []Finding {
	q, err := draft.Locate(e.SharedRoot, e.Home)
	var detail string
	switch {
	case err != nil:
		detail = err.Error() + "; ghv and ghv-apply refuse it"
	case q.Shared():
		detail = fmt.Sprintf("drafts go to the shared queue at %s (group %s)", q.Root, q.Group())
	case e.Human != nil && e.Human.UID != e.UID:
		detail = fmt.Sprintf("drafts go to %s, which %s's ghv-apply does not read; set up the shared queue (docs/containment.md)", q.Root, e.Human.Name)
	default:
		return nil
	}
	return []Finding{{Severity: Info, Detail: detail}}
}
