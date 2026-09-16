package gateway

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MrMirhan/intellyrouter/internal/jsonbytes"
)

// consultWriter relays executor event streams to the client and hides the
// ask_director tool. When the executor stops to ask the director, the writer
// holds back the end of the message, so the gateway can answer and stitch the
// executor's continuation into the same client message.
type consultWriter struct {
	tool      string // the hidden consult tool
	mu        sync.Mutex
	w         http.ResponseWriter
	rc        *http.ResponseController
	lastWrite time.Time
	started   bool // the client response has begun
	raw       bool // the first response is not an event stream and is copied unchanged
	sentStart bool
	next      int // next client content block index
	output    int // output tokens of earlier segments
	seg       *segment
}

// segment is one executor response.
type segment struct {
	continuation bool
	header       http.Header
	status       int
	failed       bool
	errBody      bytes.Buffer
	buf          []byte
	index        map[int]int // upstream block index to client block index
	blocks       map[int]*streamBlock
	order        []int
	asks         []int
	clientTools  bool
	stopReason   string
	outputTokens int
	held         []sseEvent
}

type sseEvent struct {
	name string
	data []byte
}

type streamBlock struct {
	kind  string
	text  strings.Builder
	input strings.Builder
}

// consultCall is an executor's question for the director.
type consultCall struct {
	question string
	// text is what the executor wrote before it asked.
	text string
	// clientTools is set when the same response calls tools that Claude Code runs.
	clientTools bool
}

func newConsultWriter(w http.ResponseWriter, tool string) *consultWriter {
	return &consultWriter{tool: tool, w: w, rc: http.NewResponseController(w), seg: newSegment(false)}
}

func newSegment(continuation bool) *segment {
	return &segment{continuation: continuation, header: make(http.Header), index: map[int]int{}, blocks: map[int]*streamBlock{}}
}

func (c *consultWriter) Header() http.Header {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seg.header
}

func (c *consultWriter) WriteHeader(code int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeHeader(code)
}

func (c *consultWriter) writeHeader(code int) {
	s := c.seg
	if s.status != 0 {
		return
	}
	s.status = code
	stream := code == http.StatusOK && isEventStream(s.header)
	if s.continuation {
		s.failed = !stream
		return
	}
	c.raw = !stream
	maps.Copy(c.w.Header(), s.header)
	c.w.WriteHeader(code)
	c.started = true
}

func (c *consultWriter) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writeHeader(http.StatusOK)
	s := c.seg
	switch {
	case c.raw:
		c.lastWrite = time.Now()
		return c.w.Write(p)
	case s.failed:
		if s.errBody.Len() < 1<<16 {
			s.errBody.Write(p)
		}
		return len(p), nil
	}
	s.buf = append(s.buf, bytes.ReplaceAll(p, []byte("\r"), nil)...)
	for {
		end := bytes.Index(s.buf, []byte("\n\n"))
		if end < 0 {
			return len(p), nil
		}
		ev := parseEvent(s.buf[:end])
		s.buf = s.buf[end+2:]
		if err := c.handle(ev); err != nil {
			return len(p), err
		}
	}
}

// FlushError lets relay flush through the writer; events are flushed as they are sent.
func (c *consultWriter) FlushError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.raw {
		return c.rc.Flush()
	}
	return nil
}

func parseEvent(b []byte) sseEvent {
	var ev sseEvent
	var data [][]byte
	for line := range bytes.SplitSeq(b, []byte("\n")) {
		switch {
		case bytes.HasPrefix(line, []byte("event:")):
			ev.name = string(bytes.TrimSpace(line[len("event:"):]))
		case bytes.HasPrefix(line, []byte("data:")):
			data = append(data, bytes.TrimPrefix(line[len("data:"):], []byte(" ")))
		}
	}
	ev.data = bytes.Join(data, []byte("\n"))
	return ev
}

func (c *consultWriter) handle(ev sseEvent) error {
	if ev.name == "" && len(ev.data) == 0 {
		return nil
	}
	var head struct {
		Type         string          `json:"type"`
		Index        *int            `json:"index"`
		ContentBlock json.RawMessage `json:"content_block"`
		Delta        json.RawMessage `json:"delta"`
		Usage        *struct {
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(ev.data, &head) != nil {
		return c.emit(ev)
	}
	if ev.name == "" {
		ev.name = head.Type
	}
	s := c.seg
	switch head.Type {
	case "message_start":
		if c.sentStart {
			return nil
		}
		c.sentStart = true
	case "content_block_start", "content_block_delta", "content_block_stop":
		if head.Index != nil {
			return c.block(ev, head.Type, *head.Index, head.ContentBlock, head.Delta)
		}
	case "message_delta":
		var d struct {
			StopReason string `json:"stop_reason"`
		}
		_ = json.Unmarshal(head.Delta, &d)
		s.stopReason = d.StopReason
		if head.Usage != nil {
			s.outputTokens = head.Usage.OutputTokens
		}
		if len(s.asks) > 0 {
			s.held = append(s.held, ev)
			return nil
		}
		return c.emitDelta(ev)
	case "message_stop":
		if len(s.asks) > 0 {
			s.held = append(s.held, ev)
			return nil
		}
	}
	return c.emit(ev)
}

func (c *consultWriter) block(ev sseEvent, typ string, idx int, start, delta json.RawMessage) error {
	s := c.seg
	if typ == "content_block_start" {
		var cb struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal(start, &cb)
		s.blocks[idx] = &streamBlock{kind: cb.Type}
		s.order = append(s.order, idx)
		if cb.Type == "tool_use" && cb.Name == c.tool {
			s.asks = append(s.asks, idx)
			return nil
		}
		s.clientTools = s.clientTools || cb.Type == "tool_use"
		s.index[idx] = c.next
		c.next++
	}
	b := s.blocks[idx]
	if b == nil {
		return c.emit(ev)
	}
	if typ == "content_block_delta" {
		b.add(delta)
	}
	client, visible := s.index[idx]
	if !visible {
		return nil
	}
	if client != idx {
		data, err := jsonbytes.SetField(ev.data, "index", []byte(strconv.Itoa(client)))
		if err != nil {
			return err
		}
		ev.data = data
	}
	return c.emit(ev)
}

func (b *streamBlock) add(delta json.RawMessage) {
	var d struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
	}
	if json.Unmarshal(delta, &d) != nil {
		return
	}
	switch d.Type {
	case "text_delta":
		b.text.WriteString(d.Text)
	case "input_json_delta":
		b.input.WriteString(d.PartialJSON)
	}
}

// endSegment finishes one executor response. When the response asked the
// director, it returns the question and keeps the end of the message held.
func (c *consultWriter) endSegment() (consultCall, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.seg
	if s.failed {
		c.emitError(s.errBody.Bytes())
		return consultCall{}, false
	}
	if len(s.asks) == 0 {
		return consultCall{}, false
	}
	if s.stopReason != "tool_use" {
		c.releaseHeld()
		return consultCall{}, false
	}
	call := consultCall{clientTools: s.clientTools}
	var texts []string
	for _, idx := range s.order {
		if b := s.blocks[idx]; b.kind == "text" && b.text.Len() > 0 {
			texts = append(texts, b.text.String())
		}
	}
	call.text = strings.Join(texts, "\n\n")
	var in struct {
		Question string `json:"question"`
	}
	_ = json.Unmarshal([]byte(s.blocks[s.asks[0]].input.String()), &in)
	call.question = strings.TrimSpace(in.Question)
	if call.question == "" {
		call.question = "(the executor sent no question text)"
	}
	return call, true
}

// release sends the held end of the message.
func (c *consultWriter) release() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseHeld()
}

func (c *consultWriter) releaseHeld() {
	held := c.seg.held
	c.seg.held = nil
	for _, ev := range held {
		if ev.name == "message_delta" {
			_ = c.emitDelta(ev)
		} else {
			_ = c.emit(ev)
		}
	}
}

// continueSegment prepares the writer for the executor's continuation.
func (c *consultWriter) continueSegment() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.output += c.seg.outputTokens
	c.seg = newSegment(true)
}

// fail ends the client stream with an error event.
func (c *consultWriter) fail(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.emit(sseEvent{name: "error", data: errorJSON("api_error", msg)})
}

func (c *consultWriter) emitError(body []byte) {
	var e struct {
		Type string `json:"type"`
	}
	data := bytes.TrimSpace(body)
	if json.Unmarshal(data, &e) != nil || e.Type != "error" {
		msg := string(data)
		if msg == "" {
			msg = "no response"
		}
		data = errorJSON("api_error", "executor continuation failed: "+clip(msg, 500))
	}
	_ = c.emit(sseEvent{name: "error", data: data})
}

// emitDelta sends message_delta with the output tokens of all segments.
func (c *consultWriter) emitDelta(ev sseEvent) error {
	if c.output > 0 {
		if data, err := addOutputTokens(ev.data, c.output); err == nil {
			ev.data = data
		}
	}
	return c.emit(ev)
}

func addOutputTokens(data []byte, n int) ([]byte, error) {
	spans, err := jsonbytes.TopLevel(data)
	if err != nil {
		return nil, err
	}
	sp, ok := spans["usage"]
	if !ok {
		return data, nil
	}
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(data[sp.Start:sp.End], &usage); err != nil {
		return nil, err
	}
	var out int
	if raw, ok := usage["output_tokens"]; ok {
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
	}
	usage["output_tokens"] = json.RawMessage(strconv.Itoa(out + n))
	u, err := json.Marshal(usage)
	if err != nil {
		return nil, err
	}
	return jsonbytes.SetField(data, "usage", u)
}

func (c *consultWriter) emit(ev sseEvent) error {
	var buf bytes.Buffer
	if ev.name != "" {
		buf.WriteString("event: " + ev.name + "\n")
	}
	buf.WriteString("data: ")
	buf.Write(bytes.ReplaceAll(ev.data, []byte("\n"), []byte("\ndata: ")))
	buf.WriteString("\n\n")
	c.lastWrite = time.Now()
	if _, err := c.w.Write(buf.Bytes()); err != nil {
		return err
	}
	return c.rc.Flush()
}

// keepAlive sends pings while the client waits for the director or for a
// continuation, so Claude Code does not close an idle stream.
func (c *consultWriter) keepAlive(interval time.Duration) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		ticker := time.NewTicker(interval / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				c.mu.Lock()
				if c.sentStart && !c.raw && time.Since(c.lastWrite) >= interval {
					_ = c.emit(sseEvent{name: "ping", data: []byte(`{"type": "ping"}`)})
				}
				c.mu.Unlock()
			}
		}
	})
	return func() {
		close(done)
		wg.Wait()
	}
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
