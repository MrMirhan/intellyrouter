package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"intellyrouter/internal/jsonbytes"
)

// Content is what was captured for one request: the client body and, per leg,
// the text the gateway added and the message the leg produced.
type Content struct {
	CreatedAt int64
	Input     []byte
	Legs      []LegContent
}

type LegContent struct {
	Seq    int
	Input  string
	Output []byte
}

// Captured is the stored content of one request.
type Captured struct {
	MessageCount int
	// Body is the client request; nil when only leg content was captured.
	Body []byte
	Legs []LegContent
}

// SessionItem is one request of a session export. Envelope is the body without
// messages, set when it differs from the previous captured request of the same
// agent. MessagesAdded are the messages after the history both requests share.
type SessionItem struct {
	Request       Request
	Captured      bool
	Envelope      []byte
	MessagesAdded [][]byte
	Legs          []LegContent
}

// SaveContent stores captured content for a request. Every message of the body
// is kept once as a gzip blob keyed by its SHA-256; the request keeps the list
// of hashes.
func (s *Store) SaveContent(ctx context.Context, requestID int64, c Content) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		envelopeHash, count := "", 0
		if len(c.Input) > 0 {
			envelope, messages := splitMessages(c.Input)
			var err error
			if envelopeHash, err = putBlob(ctx, tx, envelope); err != nil {
				return err
			}
			for i, m := range messages {
				hash, err := putBlob(ctx, tx, m)
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO request_messages (request_id, position, hash) VALUES (?, ?, ?)`, requestID, i, hash); err != nil {
					return err
				}
			}
			count = len(messages)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO request_content (request_id, envelope_hash, message_count, created_at) VALUES (?, ?, ?, ?)`,
			requestID, envelopeHash, count, c.CreatedAt); err != nil {
			return err
		}
		for _, l := range c.Legs {
			outputHash := ""
			if len(l.Output) > 0 {
				var err error
				if outputHash, err = putBlob(ctx, tx, l.Output); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO leg_content (request_id, seq, input, output_hash) VALUES (?, ?, ?, ?)`,
				requestID, l.Seq, l.Input, outputHash); err != nil {
				return err
			}
		}
		return nil
	})
}

// RequestContent returns the content captured for a request. tail > 0 keeps
// only the last tail messages in the body. It returns ErrNotFound when nothing
// was captured.
func (s *Store) RequestContent(ctx context.Context, requestID int64, tail int) (Captured, error) {
	var c Captured
	var envelopeHash string
	err := s.db.QueryRowContext(ctx, `SELECT envelope_hash, message_count FROM request_content WHERE request_id = ?`, requestID).
		Scan(&envelopeHash, &c.MessageCount)
	if errors.Is(err, sql.ErrNoRows) {
		return Captured{}, ErrNotFound
	}
	if err != nil {
		return Captured{}, err
	}
	from := 0
	if tail > 0 && tail < c.MessageCount {
		from = c.MessageCount - tail
	}
	var hashes []string
	err = s.each(ctx, `SELECT hash FROM request_messages WHERE request_id = ? AND position >= ? ORDER BY position`,
		[]any{requestID, from}, func(rows *sql.Rows) error {
			var h string
			if err := rows.Scan(&h); err != nil {
				return err
			}
			hashes = append(hashes, h)
			return nil
		})
	if err != nil {
		return Captured{}, err
	}
	legs, err := s.legContentRows(ctx, `r.id = ?`, requestID)
	if err != nil {
		return Captured{}, err
	}
	need := append([]string{envelopeHash}, hashes...)
	for _, l := range legs {
		need = append(need, l.outputHash)
	}
	data, err := s.blobs(ctx, need)
	if err != nil {
		return Captured{}, err
	}
	if envelopeHash != "" {
		messages := make([][]byte, len(hashes))
		for i, h := range hashes {
			messages[i] = data[h]
		}
		c.Body = joinMessages(data[envelopeHash], messages)
	}
	for _, l := range legs {
		c.Legs = append(c.Legs, LegContent{Seq: l.seq, Input: l.input, Output: data[l.outputHash]})
	}
	return c, nil
}

// SessionContent returns the requests of a session in order with their
// captured content as deltas; see SessionItem.
func (s *Store) SessionContent(ctx context.Context, sessionID string) ([]SessionItem, error) {
	requests, err := s.SessionRequests(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	type captured struct {
		envelope string
		hashes   []string
	}
	content := make(map[int64]*captured)
	err = s.each(ctx, `SELECT c.request_id, c.envelope_hash FROM request_content c JOIN requests r ON r.id = c.request_id WHERE r.session_id = ?`,
		[]any{sessionID}, func(rows *sql.Rows) error {
			var id int64
			var c captured
			if err := rows.Scan(&id, &c.envelope); err != nil {
				return err
			}
			content[id] = &c
			return nil
		})
	if err != nil {
		return nil, err
	}
	err = s.each(ctx, `SELECT m.request_id, m.hash FROM request_messages m JOIN requests r ON r.id = m.request_id
WHERE r.session_id = ? ORDER BY m.request_id, m.position`,
		[]any{sessionID}, func(rows *sql.Rows) error {
			var id int64
			var h string
			if err := rows.Scan(&id, &h); err != nil {
				return err
			}
			if c := content[id]; c != nil {
				c.hashes = append(c.hashes, h)
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	legRows, err := s.legContentRows(ctx, `r.session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	legs := make(map[int64][]legContentRow)
	for _, l := range legRows {
		legs[l.requestID] = append(legs[l.requestID], l)
	}

	items := make([]SessionItem, len(requests))
	envelopes := make([]string, len(requests))
	added := make([][]string, len(requests))
	var need []string
	prev := make(map[string]captured)
	for i, r := range requests {
		items[i].Request = r
		c := content[r.ID]
		if c == nil {
			continue
		}
		items[i].Captured = true
		if c.envelope != "" {
			p, seen := prev[r.AgentID]
			if !seen || p.envelope != c.envelope {
				envelopes[i] = c.envelope
				need = append(need, c.envelope)
			}
			added[i] = c.hashes[commonPrefix(p.hashes, c.hashes):]
			need = append(need, added[i]...)
			prev[r.AgentID] = *c
		}
		for _, l := range legs[r.ID] {
			need = append(need, l.outputHash)
		}
	}
	data, err := s.blobs(ctx, need)
	if err != nil {
		return nil, err
	}
	for i := range items {
		c := content[items[i].Request.ID]
		if c == nil {
			continue
		}
		if envelopes[i] != "" {
			items[i].Envelope = data[envelopes[i]]
		}
		if c.envelope != "" {
			items[i].MessagesAdded = make([][]byte, 0, len(added[i]))
			for _, h := range added[i] {
				items[i].MessagesAdded = append(items[i].MessagesAdded, data[h])
			}
		}
		items[i].Legs = []LegContent{}
		for _, l := range legs[items[i].Request.ID] {
			items[i].Legs = append(items[i].Legs, LegContent{Seq: l.seq, Input: l.input, Output: data[l.outputHash]})
		}
	}
	return items, nil
}

// PruneContent deletes content captured before the given Unix ms time, then
// the blobs that nothing references any more.
func (s *Store) PruneContent(ctx context.Context, before int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		for _, q := range []string{
			`DELETE FROM request_messages WHERE request_id IN (SELECT request_id FROM request_content WHERE created_at < ?)`,
			`DELETE FROM leg_content WHERE request_id IN (SELECT request_id FROM request_content WHERE created_at < ?)`,
			`DELETE FROM request_content WHERE created_at < ?`,
		} {
			if _, err := tx.ExecContext(ctx, q, before); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM content_blobs
WHERE hash NOT IN (SELECT hash FROM request_messages)
  AND hash NOT IN (SELECT envelope_hash FROM request_content)
  AND hash NOT IN (SELECT output_hash FROM leg_content)`)
		return err
	})
}

type legContentRow struct {
	requestID  int64
	seq        int
	input      string
	outputHash string
}

func (s *Store) legContentRows(ctx context.Context, where string, args ...any) ([]legContentRow, error) {
	var out []legContentRow
	err := s.each(ctx, `SELECT lc.request_id, lc.seq, lc.input, lc.output_hash FROM leg_content lc JOIN requests r ON r.id = lc.request_id
WHERE `+where+` ORDER BY lc.request_id, lc.seq`, args, func(rows *sql.Rows) error {
		var l legContentRow
		if err := rows.Scan(&l.requestID, &l.seq, &l.input, &l.outputHash); err != nil {
			return err
		}
		out = append(out, l)
		return nil
	})
	return out, err
}

// splitMessages returns body with messages replaced by [] and the raw bytes of
// each message. A body without a messages array is kept whole.
func splitMessages(body []byte) ([]byte, []json.RawMessage) {
	spans, err := jsonbytes.TopLevel(body)
	sp, ok := spans["messages"]
	if err != nil || !ok {
		return body, nil
	}
	var messages []json.RawMessage
	if err := json.Unmarshal(body[sp.Start:sp.End], &messages); err != nil || messages == nil {
		return body, nil
	}
	envelope, err := jsonbytes.SetField(body, "messages", []byte("[]"))
	if err != nil {
		return body, nil
	}
	return envelope, messages
}

// joinMessages puts messages back into an envelope made by splitMessages.
func joinMessages(envelope []byte, messages [][]byte) []byte {
	spans, err := jsonbytes.TopLevel(envelope)
	if _, ok := spans["messages"]; err != nil || !ok {
		return envelope
	}
	list := append([]byte{'['}, bytes.Join(messages, []byte{','})...)
	body, err := jsonbytes.SetField(envelope, "messages", append(list, ']'))
	if err != nil {
		return envelope
	}
	return body
}

func commonPrefix(a, b []string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// putBlob stores data unless a blob with the same hash exists and returns the hash.
func putBlob(ctx context.Context, tx *sql.Tx, data []byte) (string, error) {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM content_blobs WHERE hash = ?`, hash).Scan(&exists)
	if err == nil {
		return hash, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		return "", err
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO content_blobs (hash, data, size) VALUES (?, ?, ?)`, hash, buf.Bytes(), len(data))
	return hash, err
}

// blobs returns the uncompressed data stored under each non-empty hash.
func (s *Store) blobs(ctx context.Context, hashes []string) (map[string][]byte, error) {
	seen := make(map[string]bool, len(hashes))
	unique := make([]any, 0, len(hashes))
	for _, h := range hashes {
		if h != "" && !seen[h] {
			seen[h] = true
			unique = append(unique, h)
		}
	}
	out := make(map[string][]byte, len(unique))
	for chunk := range slices.Chunk(unique, 500) {
		err := s.each(ctx, `SELECT hash, data FROM content_blobs WHERE hash IN (`+placeholders(len(chunk))+`)`, chunk, func(rows *sql.Rows) error {
			var hash string
			var data []byte
			if err := rows.Scan(&hash, &data); err != nil {
				return err
			}
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return err
			}
			plain, err := io.ReadAll(zr)
			if err != nil {
				return err
			}
			out[hash] = plain
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
