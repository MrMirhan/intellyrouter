package gateway

import (
	"bytes"
	"io"
	"net/http"
)

// restoringWriter sends a response body through a nameRestorer while leaving
// the status and headers to the real writer.
type restoringWriter struct {
	http.ResponseWriter
	body *nameRestorer
}

func (r *restoringWriter) Write(p []byte) (int, error) { return r.body.Write(p) }

func (r *restoringWriter) FlushError() error {
	return http.NewResponseController(r.ResponseWriter).Flush()
}

// releaseOnContent and fail belong to a writer that holds the response. This
// wrapper sits between the relay and that writer, so it passes both through.
func (r *restoringWriter) releaseOnContent() {
	if rel, ok := r.ResponseWriter.(streamReleaser); ok {
		rel.releaseOnContent()
	}
}

func (r *restoringWriter) fail(reason string) {
	if f, ok := r.ResponseWriter.(interface{ fail(string) }); ok {
		f.fail(reason)
	}
}

// nameRestorer puts the original tool names back into a response whose request
// had them shortened for a provider with a lower name limit. The executor
// answers with the short name, but the client only knows the long one.
//
// A short name is a plain token that ends in a hash of the original, so it
// cannot appear in the response by chance and a byte replacement is enough.
// The writer holds back the last few bytes of each chunk in case a name
// straddles a chunk boundary.
type nameRestorer struct {
	w     io.Writer
	names map[string]string
	hold  []byte
	// keep is the longest short name, which is how far back a name can start
	// and still be cut in half by the end of a chunk.
	keep int
}

func newNameRestorer(w io.Writer, names map[string]string) *nameRestorer {
	keep := 0
	for short := range names {
		if len(short) > keep {
			keep = len(short)
		}
	}
	return &nameRestorer{w: w, names: names, keep: keep}
}

func (n *nameRestorer) Write(p []byte) (int, error) {
	buf := n.replace(append(n.hold, p...))
	// Whatever is left of a short name can only be a prefix of one sitting at
	// the very end, so hold back exactly that much and send the rest.
	hold := n.danglingPrefix(buf)
	out, tail := buf[:len(buf)-hold], buf[len(buf)-hold:]
	n.hold = append(n.hold[:0], tail...)
	if len(out) == 0 {
		return len(p), nil
	}
	if _, err := n.w.Write(out); err != nil {
		return 0, err
	}
	return len(p), nil
}

// danglingPrefix returns how many bytes at the end of b begin a short name
// without finishing it, so the next chunk can complete the match.
func (n *nameRestorer) danglingPrefix(b []byte) int {
	max := n.keep - 1
	if max > len(b) {
		max = len(b)
	}
	for size := max; size > 0; size-- {
		tail := b[len(b)-size:]
		for short := range n.names {
			if len(short) > size && bytes.HasPrefix([]byte(short), tail) {
				return size
			}
		}
	}
	return 0
}

// Flush writes what the writer held back for the next chunk.
func (n *nameRestorer) Flush() error {
	if len(n.hold) == 0 {
		return nil
	}
	out := n.replace(n.hold)
	n.hold = n.hold[:0]
	_, err := n.w.Write(out)
	return err
}

func (n *nameRestorer) replace(b []byte) []byte {
	for short, long := range n.names {
		if !bytes.Contains(b, []byte(short)) {
			continue
		}
		b = bytes.ReplaceAll(b, []byte(short), []byte(long))
	}
	return b
}
