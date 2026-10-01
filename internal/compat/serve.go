package compat

import (
	"io"
	"net/http"
	"strings"
)

// ServeStdio runs the fake server over newline-delimited JSON-RPC until
// input EOF (in-process equivalent of the fakecmd binary).
func (s *Server) ServeStdio(in io.Reader, out io.Writer) error {
	buf := make([]byte, 0, 64*1024)
	chunk := make([]byte, 32*1024)
	var pending []byte
	for {
		n, err := in.Read(chunk)
		pending = append(pending, chunk[:n]...)
		for {
			i := strings.IndexByte(string(pending), '\n')
			if i < 0 {
				break
			}
			line := pending[:i]
			pending = pending[i+1:]
			if len(line) == 0 {
				continue
			}
			if resp := s.Handle(append(buf[:0], line...)); len(resp) > 0 {
				if _, werr := out.Write(resp); werr != nil {
					return werr
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// HTTPHandler serves the same era-variant behavior over streamable HTTP
// (single POST endpoint, JSON or SSE responses), recording the
// request-metadata headers on every request.
func (s *Server) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.ObserveHeaders(map[string]string{
			"MCP-Protocol-Version": r.Header.Get("MCP-Protocol-Version"),
			"Mcp-Method":           r.Header.Get("Mcp-Method"),
			"Mcp-Name":             r.Header.Get("Mcp-Name"),
		})
		body, err := io.ReadAll(io.LimitReader(r.Body, 16*1024*1024))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		resp := s.Handle(body)
		w.Header().Set("Content-Type", "application/json")
		if len(resp) == 0 {
			// Notification accepted: 202 with empty body (spec §Streamable
			// HTTP "Sending Messages").
			w.WriteHeader(http.StatusAccepted)
			return
		}
		_, _ = w.Write(resp)
	})
	return mux
}
