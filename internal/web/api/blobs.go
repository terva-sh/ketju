package api

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/terva-sh/ketju/internal/domain"
	"github.com/terva-sh/ketju/internal/problem"
	"github.com/terva-sh/ketju/internal/service/blobs"
	"github.com/terva-sh/ketju/internal/store/threadid"
)

// upload is POST /api/v1/blobs?thread={id} or ?forum={slug} (docs/07
// "Attachments", TKT-01M29CXK0K): one multipart part named file, stored and
// answered 201 as a dto.Attachment.
//
// Whether the viewer may upload there is asked before a byte of the body
// is read. The parts are read as a stream with MultipartReader, never
// ParseMultipartForm, which would buffer the file to memory or a temporary
// file of its own before anything checked it. The body limit is the
// chain's (web.Limits, the attachment cap plus a multipart allowance), and
// the file part itself is capped at exactly the attachment cap by the
// service while it is read.
//
// Any part other than one named file is bad-request. The file part is read
// through onlyPart, which looks for the end of the body when the part ends
// and fails the read if another part follows, so the service refuses the
// upload before it stores anything: a rejected request leaves no blob and
// no upload row behind (Terva review 1736).
func (h *handlers) upload(w http.ResponseWriter, r *http.Request) {
	if h.blobs == nil {
		problem.Write(w, r, problem.CodeNotImplemented)
		return
	}
	v := domain.ViewerFrom(r.Context())
	t, err := uploadTarget(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if _, err := h.blobs.MayUpload(r.Context(), v, t); err != nil {
		h.fail(w, r, err)
		return
	}
	mr, err := r.MultipartReader()
	if err != nil {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: an upload is multipart/form-data: %w", err)))
		return
	}
	part, err := mr.NextPart()
	if err != nil {
		h.fail(w, r, partError(err, "api: the upload has no file part"))
		return
	}
	if part.FormName() != "file" {
		h.fail(w, r, problem.Coded(problem.CodeBadRequest, fmt.Errorf("api: the upload's part is %q, not file", part.FormName())))
		return
	}
	att, err := h.blobs.Upload(r.Context(), v, t, part.FileName(), &onlyPart{part: part, mr: mr})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, att)
}

// onlyPart reads the file part, and at its end reads on to the end of the
// multipart body: io.EOF only when nothing follows, and otherwise the error
// partError makes of what does, so the upload fails before Commit.
type onlyPart struct {
	part io.Reader
	mr   *multipart.Reader
	err  error
}

func (o *onlyPart) Read(p []byte) (int, error) {
	if o.err != nil {
		return 0, o.err
	}
	n, err := o.part.Read(p)
	if !errors.Is(err, io.EOF) {
		return n, err
	}
	o.err = io.EOF
	if _, next := o.mr.NextPart(); !errors.Is(next, io.EOF) {
		o.err = partError(next, "api: an upload takes one part")
	}
	return n, o.err
}

// partError is a multipart read's failure: a body over the chain's limit
// keeps its *http.MaxBytesError, and so its payload-too-large; anything else
// is a body that is not what it says, bad-request.
func partError(err error, what string) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	if err == nil || errors.Is(err, io.EOF) {
		return problem.Coded(problem.CodeBadRequest, errors.New(what))
	}
	return problem.Coded(problem.CodeBadRequest, fmt.Errorf("%s: %w", what, err))
}

// uploadTarget reads ?thread= or ?forum=, exactly one of them.
func uploadTarget(r *http.Request) (blobs.Target, error) {
	q := r.URL.Query()
	switch {
	case q.Has("thread") && q.Has("forum"), !q.Has("thread") && !q.Has("forum"):
		return blobs.Target{}, problem.Coded(problem.CodeBadRequest, errors.New("api: an upload names ?thread= or ?forum="))
	case q.Has("thread"):
		id, err := threadid.Parse(q.Get("thread"))
		if err != nil {
			return blobs.Target{}, err
		}
		return blobs.Target{Thread: id, HasThread: true}, nil
	default:
		if q.Get("forum") == "" {
			return blobs.Target{}, problem.Coded(problem.CodeBadRequest, errors.New("api: ?forum= is empty"))
		}
		return blobs.Target{Forum: q.Get("forum")}, nil
	}
}
