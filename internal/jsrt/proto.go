package jsrt

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// The host and a worker exchange length-prefixed JSON frames over the
// worker's stdin/stdout: a 4-byte big-endian payload length, then the JSON
// payload. One call is in flight per worker; during a call the worker may
// send callback frames ("cb") that the host answers with "reply" frames
// carrying the same id, before it sends the "result".

// maxFrame bounds a frame so a corrupt length cannot make either side
// allocate without limit.
const maxFrame = 256 << 20

type frame struct {
	T      string     `json:"t"` // ready | call | result | cb | reply
	Ready  *readyMsg  `json:"ready,omitempty"`
	Call   *callMsg   `json:"call,omitempty"`
	Result *resultMsg `json:"result,omitempty"`
	CB     *cbMsg     `json:"cb,omitempty"`
	Reply  *replyMsg  `json:"reply,omitempty"`
}

type readyMsg struct {
	PID      int    `json:"pid"`
	Lockdown string `json:"lockdown"`
}

// callMsg asks the worker to evaluate one binding.
type callMsg struct {
	Expr       bool                `json:"expr,omitempty"` // Source is a j: expression
	Source     string              `json:"source"`
	Kind       string              `json:"kind"`
	ArgCount   int                 `json:"argc"` // -1 = any
	DocMode    docMode             `json:"doc"`  // ambient document mode
	HasContext bool                `json:"hasContext,omitempty"`
	HTTPResp   bool                `json:"httpResponse,omitempty"`
	This       string              `json:"this,omitempty"` // wire JSON; "" = undefined
	Args       string              `json:"args"`           // wire JSON array
	Scope      []string            `json:"scope,omitempty"`
	Context    string              `json:"context,omitempty"` // wire JSON object
	Request    string              `json:"request,omitempty"` // wire JSON (tagged request)
	Elements   []wireElem          `json:"elements,omitempty"`
	Doc        *wireDoc            `json:"docInfo,omitempty"`
	Schemas    []*SchemaDescriptor `json:"schemas,omitempty"`
	ValueTypes []string            `json:"valueTypes,omitempty"` // schema types of instances/classes in the inputs
	TimeoutNS  int64               `json:"timeout"`
	Memory     int64               `json:"memory"`
	DOMOps     int                 `json:"domOps,omitempty"`
}

// wireDoc is the ambient document.
type wireDoc struct {
	HTML    string `json:"html"`
	XML     bool   `json:"xml,omitempty"`
	Source  string `json:"source,omitempty"`
	Root    bool   `json:"root,omitempty"` // HTML is one element: the documentElement of the view
	Context string `json:"ctx,omitempty"`  // parent tag of that element, for fragment parsing
	Marked  bool   `json:"marked,omitempty"`
	// NS holds the xmlns:<prefix> declarations in scope above the view root.
	NS map[string]string `json:"ns,omitempty"`
}

// resultMsg is the outcome of a call.
type resultMsg struct {
	Outcome         string             `json:"outcome"` // "ok" or a Variant
	Message         string             `json:"message,omitempty"`
	Stack           string             `json:"stack,omitempty"`
	Specifier       string             `json:"specifier,omitempty"`
	Response        *wireResponse      `json:"response,omitempty"`
	Value           string             `json:"value,omitempty"`
	Elements        []resultElem       `json:"elements,omitempty"`
	Document        string             `json:"document,omitempty"`
	DocumentChanged bool               `json:"documentChanged,omitempty"`
	ContextWrites   []wireContextWrite `json:"contextWrites,omitempty"`
	Tainted         bool               `json:"tainted,omitempty"`
	ElapsedNS       int64              `json:"elapsed"`
	MemoryUsed      int64              `json:"memoryUsed"`
	DOMOps          int                `json:"domOps"`
	HostCalls       int                `json:"hostCalls"`
	RSS             int64              `json:"rss"`
}

type wireResponse struct {
	Status  int         `json:"status"`
	Message string      `json:"message"`
	Body    string      `json:"body"`
	Headers [][2]string `json:"headers,omitempty"`
}

type wireContextWrite struct {
	Name     string       `json:"name"`
	Value    string       `json:"value,omitempty"`
	Elements []resultElem `json:"elements,omitempty"`
	Deleted  bool         `json:"deleted,omitempty"`
}

// cbMsg is a callback from the worker.
type cbMsg struct {
	ID       int          `json:"id"`
	Op       string       `json:"op"` // schema | method | context
	Type     string       `json:"type,omitempty"`
	Method   string       `json:"method,omitempty"`
	Static   bool         `json:"static,omitempty"`
	Value    string       `json:"value,omitempty"` // method: [receiver, ...args]; context: the value
	Elements []resultElem `json:"elements,omitempty"`
	Name     string       `json:"name,omitempty"`
	Deleted  bool         `json:"deleted,omitempty"`
}

// replyMsg answers a callback. Value and the Context values are in the
// input encoding and share the Elements table.
type replyMsg struct {
	ID       int               `json:"id"`
	Error    string            `json:"error,omitempty"`
	Schema   *SchemaDescriptor `json:"schema,omitempty"`
	Value    string            `json:"value,omitempty"`
	Elements []wireElem        `json:"elements,omitempty"`
	Context  []wireCtxIn       `json:"context,omitempty"`
}

type wireCtxIn struct {
	Name    string `json:"name"`
	Value   string `json:"value,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

// conn reads and writes frames.
type conn struct {
	r  *bufio.Reader
	w  *bufio.Writer
	mu sync.Mutex
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{r: bufio.NewReaderSize(r, 64<<10), w: bufio.NewWriterSize(w, 64<<10)}
}

func (c *conn) send(f *frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(b) > maxFrame {
		return fmt.Errorf("jsrt: frame of %d bytes exceeds the %d byte limit", len(b), maxFrame)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := c.w.Write(hdr[:]); err != nil {
		return err
	}
	if _, err := c.w.Write(b); err != nil {
		return err
	}
	return c.w.Flush()
}

func (c *conn) recv() (*frame, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrame {
		return nil, fmt.Errorf("jsrt: frame of %d bytes exceeds the %d byte limit", n, maxFrame)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(c.r, b); err != nil {
		return nil, err
	}
	var f frame
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("jsrt: bad frame: %w", err)
	}
	return &f, nil
}
