package threads

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/forms"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// normalizedTags is a request's tags checked one by one: the set a thread
// would store, folded, each once and sorted, and beside each request index
// the code it is refused with, or "" for a tag that passed. tooMany is more
// than domain.MaxTags distinct tags among those that passed.
type normalizedTags struct {
	tags    []string
	norm    []string
	codes   []string
	tooMany bool
}

func normalizeTags(in []string) normalizedTags {
	n := normalizedTags{tags: []string{}, norm: make([]string, len(in)), codes: make([]string, len(in))}
	for i, raw := range in {
		t, err := domain.NormalizeTag(raw)
		switch {
		case errors.Is(err, domain.ErrTagTooLong):
			n.codes[i] = forms.CodeTooLong
		case err != nil:
			n.codes[i] = forms.CodeInvalidTag
		default:
			n.norm[i] = t
			n.tags = append(n.tags, t)
		}
	}
	slices.Sort(n.tags)
	n.tags = slices.Compact(n.tags)
	n.tooMany = len(n.tags) > domain.MaxTags
	return n
}

// refusal is the validation-failed the codes describe, in request order and
// then tags too_many, or nil when nothing was refused.
func (n normalizedTags) refusal() error {
	var bad []forms.FieldError
	for i, c := range n.codes {
		if c != "" {
			bad = append(bad, forms.FieldError{Field: "tags." + strconv.Itoa(i), Code: c})
		}
	}
	if n.tooMany {
		bad = append(bad, forms.FieldError{Field: "tags", Code: forms.CodeTooMany})
	}
	if bad == nil {
		return nil
	}
	return &forms.Invalid{Kind: forms.ErrInvalidAnswers, Fields: bad}
}

// NormalizeTags is a thread's tag set as the log records it: each tag
// through domain.NormalizeTag, a repeat kept once, sorted (docs/04
// `thread_tags`). A refused tag is validation-failed naming `tags.N`, N its
// index in in, with invalid_tag or too_long, and more than domain.MaxTags
// distinct tags is `tags` too_many; every error is listed, in index order.
// The answer is never nil, so an empty set records as one.
//
// Whether the viewer may set a reserved tag is the caller's question.
func NormalizeTags(in []string) ([]string, error) {
	n := normalizeTags(in)
	if err := n.refusal(); err != nil {
		return nil, err
	}
	return n.tags, nil
}

// checkTags is NormalizeTags for a new thread in forumID, plus the reserved
// rule (docs/08 "Thread tags"): a reserved tag is refused with reserved_tag
// at its index unless v holds moderate in the forum, by role or through a
// group, or the account is an agent's, which flags, the frame flags the
// records will carry, says. perm is asked only when a reserved tag was sent.
func (s *Service) checkTags(ctx context.Context, v domain.Viewer, forumID int64, flags threadlog.FrameFlags, in []string) ([]string, error) {
	n := normalizeTags(in)
	if flags&threadlog.FrameFlagMachineAuthored == 0 && slices.ContainsFunc(n.norm, domain.IsReservedTag) {
		mod, err := s.Perm.Can(ctx, v, forumID, domain.PermModerate)
		if err != nil {
			return nil, err
		}
		if !mod {
			for i, t := range n.norm {
				if n.codes[i] == "" && domain.IsReservedTag(t) {
					n.codes[i] = forms.CodeReservedTag
				}
			}
		}
	}
	if err := n.refusal(); err != nil {
		return nil, err
	}
	return n.tags, nil
}
