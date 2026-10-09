package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// msg is a JSON-RPC 2.0 message. Exactly one of Method (request or
// notification) and Result/Error (response) is set.
type msg struct {
	ID     json.RawMessage
	Method string
	Params json.RawMessage
	Result json.RawMessage
	Error  json.RawMessage
}

func (m *msg) isResponse() bool { return m.Method == "" }
func (m *msg) isRequest() bool  { return m.Method != "" && m.ID != nil }

func (m *msg) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`{"jsonrpc":"2.0"`)
	if m.ID != nil {
		b.WriteString(`,"id":`)
		b.Write(m.ID)
	}
	if m.Method != "" {
		b.WriteString(`,"method":`)
		q, _ := json.Marshal(m.Method)
		b.Write(q)
		if m.Params != nil {
			b.WriteString(`,"params":`)
			b.Write(m.Params)
		}
	} else if m.Error != nil {
		b.WriteString(`,"error":`)
		b.Write(m.Error)
	} else {
		b.WriteString(`,"result":`)
		if m.Result == nil {
			b.WriteString("null")
		} else {
			b.Write(m.Result)
		}
	}
	b.WriteString("}")
	return b.Bytes(), nil
}

func (m *msg) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if string(raw.ID) == "null" {
		raw.ID = nil
	}
	*m = msg{ID: raw.ID, Method: raw.Method, Params: raw.Params, Result: raw.Result, Error: raw.Error}
	return nil
}

// conn reads and writes LSP base-protocol framed messages.
type conn struct {
	r  *bufio.Reader
	w  io.Writer
	mu sync.Mutex
}

func newConn(r io.Reader, w io.Writer) *conn {
	return &conn{r: bufio.NewReader(r), w: w}
}

func (c *conn) read() (*msg, error) {
	length := -1
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, err = strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("bad Content-Length: %q", line)
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(c.r, body); err != nil {
		return nil, err
	}
	m := &msg{}
	if err := json.Unmarshal(body, m); err != nil {
		return nil, fmt.Errorf("bad message: %w", err)
	}
	return m, nil
}

func (c *conn) write(m *msg) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
