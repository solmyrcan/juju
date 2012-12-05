package rpc

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

type encoder interface {
	Encode(e interface{}) error
}

type decoder interface {
	Decode(e interface{}) error
}

type generalServerCodec struct {
	enc encoder
	dec decoder
}

func (c *generalServerCodec) ReadRequestHeader(req *Request) error {
	return c.dec.Decode(req)
}

func (c *generalServerCodec) ReadRequestBody(argp interface{}) error {
	if argp == nil {
		argp = &struct{}{}
	}
	return c.dec.Decode(argp)
}

func (c *generalServerCodec) WriteResponse(resp *Response, v interface{}) error {
	if err := c.enc.Encode(resp); err != nil {
		return err
	}
	return c.enc.Encode(v)
}

type generalClientCodec struct {
	enc encoder
	dec decoder
}

func (c *generalClientCodec) WriteRequest(req *Request, x interface{}) error {
	if err := c.enc.Encode(req); err != nil {
		return err
	}
	return c.enc.Encode(x)
}

func (c *generalClientCodec) ReadResponseHeader(resp *Response) error {
	return c.dec.Decode(resp)
}

func (c *generalClientCodec) ReadResponseBody(r interface{}) error {
	if r == nil {
		r = &struct{}{}
	}
	return c.dec.Decode(r)
}

func NewJSONServerCodec(c io.ReadWriter) ServerCodec {
	return &generalServerCodec{
		enc: json.NewEncoder(c),
		dec: json.NewDecoder(c),
	}
}

func NewJSONClientCodec(c io.ReadWriter) ClientCodec {
	return &generalClientCodec{
		enc: json.NewEncoder(c),
		dec: json.NewDecoder(c),
	}
}

func NewXMLServerCodec(c io.ReadWriter) ServerCodec {
	return &generalServerCodec{
		enc: xml.NewEncoder(c),
		dec: xml.NewDecoder(c),
	}
}

func NewXMLClientCodec(c io.ReadWriter) ClientCodec {
	return &generalClientCodec{
		enc: xml.NewEncoder(c),
		dec: xml.NewDecoder(c),
	}
}

type httpClientCodec struct {
	url string
	// TODO allow more than one request at a time.
	currentSeq      uint64
	currentResponse *http.Response
	// TODO close when done, even if ReadResponseBody not called.
}

func NewHTTPClientCodec(url string) ClientCodec {
	// strip trailing slash so we can always append a
	// slash-rooted path.
	if url[len(url)-1] == '/' {
		url = url[0 : len(url)-1]
	}
	return &httpClientCodec{
		url: url,
	}
}

func isJSONResponse(resp *http.Response) bool {
	return resp.Header.Get("Content-Type") == "application/json"
}

func (c *httpClientCodec) WriteRequest(req *Request, x interface{}) error {
	if req.Path == "" || req.Path[0] != '/' {
		return fmt.Errorf("bad path in RPC request: %q", req.Path)
	}
	data, err := json.Marshal(x)
	if err != nil {
		return err
	}
	resp, err := http.PostForm(c.url+req.Path, url.Values{"p": {string(data)}})
	if err != nil {
		return err
	}
	c.currentSeq = req.Seq
	c.currentResponse = resp
	return nil
}

func (c *httpClientCodec) ReadResponseHeader(resp *Response) error {
	hresp := c.currentResponse
	if hresp.StatusCode != http.StatusOK {
		if hresp.StatusCode != http.StatusBadRequest || !isJSONResponse(hresp) {
			// TODO include some of error response in returned error?
			return fmt.Errorf("http error: %v", hresp.Status)
		}
		var e jsonError
		dec := json.NewDecoder(c.currentResponse.Body)
		if err := dec.Decode(&e); err != nil {
			return err
		}
		resp.Error = e.Error
		resp.ErrorPath = e.ErrorPath
		return nil
	}
	resp.Seq = c.currentSeq
	return nil
}

func (c *httpClientCodec) ReadResponseBody(r interface{}) error {
	hresp := c.currentResponse
	defer hresp.Body.Close()
	c.currentResponse = nil
	if hresp.StatusCode != http.StatusOK {
		return nil
	}
	if r == nil {
		r = &struct{}{}
	}
	dec := json.NewDecoder(hresp.Body)
	err := dec.Decode(r)
	return err
}

type rpcHTTPHandler struct {
	srv        *Server
	newContext func(req *http.Request) interface{}
}

// NewHTTPHandler returns an HTTP handler that serves HTTP POST requests
// by treating them as RPC calls.
//
// The arguments to an RPC are read, in JSON-encoded form, from the "p"
// form parameter.  The response is written in JSON format.
//
// TODO encode struct fields directly in the form?
func (srv *Server) NewHTTPHandler(newContext func(req *http.Request) interface{}) http.Handler {
	if newContext == nil {
		newContext = func(*http.Request) interface{} { return nil }
	}
	return &rpcHTTPHandler{
		srv:        srv,
		newContext: newContext,
	}
}

func (h *rpcHTTPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// TODO POST vs GET
	ctxt := h.newContext(req)
	codec := newHTTPServerCodec(w, req)
	err := h.srv.ServeCodec(codec, ctxt)
	if err != nil {
		log.Printf("ServeCodec error: %v", err)
	}
}

type httpServerCodec struct {
	done bool
	w    http.ResponseWriter
	req  *http.Request
}

func newHTTPServerCodec(w http.ResponseWriter, req *http.Request) ServerCodec {
	return &httpServerCodec{
		w:   w,
		req: req,
	}
}

func (c *httpServerCodec) ReadRequestHeader(req *Request) error {
	if c.done {
		return io.EOF
	}
	c.done = true
	req.Path = c.req.URL.Path
	req.Seq = 0
	return nil
}

func (c *httpServerCodec) ReadRequestBody(argp interface{}) error {
	if argp == nil {
		return nil
	}
	return json.Unmarshal([]byte(c.req.FormValue("p")), argp)
}

type jsonError struct {
	Error     string
	ErrorPath string
}

func (c *httpServerCodec) WriteResponse(resp *Response, v interface{}) error {
	var data []byte
	c.w.Header().Set("Content-Type", "application/json")
	if resp.Error != "" {
		c.w.WriteHeader(http.StatusBadRequest)
		v = &jsonError{
			Error:     resp.Error,
			ErrorPath: resp.ErrorPath,
		}
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	_, err = c.w.Write(data)
	return err
}
