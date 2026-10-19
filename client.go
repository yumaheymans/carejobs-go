package carejobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// Version is the version of this module, sent in the default User-Agent.
	Version = "1.0.0"

	// DefaultBaseURL is the CareJobs.Now site; the MCP endpoint is
	// DefaultBaseURL + "/api/mcp".
	DefaultBaseURL = "https://carejobs.now"

	// DefaultTimeout is how long a call may take when the context carries no
	// deadline of its own. A context with a deadline overrides it, so pass a
	// longer one for a slow query: the server answers a query it has not seen
	// before in up to about 20 seconds, then repeats of it in a fraction of one.
	DefaultTimeout = 15 * time.Second

	// maxResponseBytes caps what one answer may read into memory. The largest
	// real answer (the whole job market) is about 135 KB.
	maxResponseBytes = 16 << 20
)

// Client calls the CareJobs.Now healthcare jobs API (a remote MCP server, no
// key needed). Create one with NewClient. A Client is safe for concurrent use.
type Client struct {
	baseURL    string
	httpClient *http.Client
	userAgent  string
	// timeout applies to calls whose context has no deadline. Zero (after
	// WithHTTPClient) leaves the timeouts to the caller's HTTP client.
	timeout time.Duration
	nextID  atomic.Int64
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the Client at another host than DefaultBaseURL (a mirror
// or a test server). A trailing slash is ignored.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithHTTPClient makes the Client use h instead of its default HTTP client. The
// DefaultTimeout then no longer applies: h (its Timeout and Transport) and the
// context of each call decide how long a call may take. A nil h is ignored.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
			c.timeout = 0
		}
	}
}

// WithUserAgent sets the User-Agent header. The default is "carejobs-go/" and
// the Version.
func WithUserAgent(userAgent string) Option {
	return func(c *Client) { c.userAgent = userAgent }
}

// NewClient returns a Client for the live API, adjusted by opts. Without
// WithHTTPClient, a call gives up after DefaultTimeout (15 seconds) unless its
// context has a deadline.
func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{},
		userAgent:  "carejobs-go/" + Version,
		timeout:    DefaultTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SearchJobs searches the open healthcare jobs. Every filter is optional.
func (c *Client) SearchJobs(ctx context.Context, p SearchParams) (*SearchResult, error) {
	var out SearchResult
	text, err := c.callTool(ctx, "search_healthcare_jobs", p, &out)
	if err != nil {
		return nil, err
	}
	out.Text = text
	return &out, nil
}

// Pay measures what healthcare jobs post as hourly pay. See PayParams for how
// the parameters choose the answer and PayResult for which fields each answer
// fills.
func (c *Client) Pay(ctx context.Context, p PayParams) (*PayResult, error) {
	var out PayResult
	text, err := c.callTool(ctx, "get_healthcare_pay", p, &out)
	if err != nil {
		return nil, err
	}
	out.Text = text
	return &out, nil
}

// JobMarket reports how many healthcare jobs are open right now and how they
// split by kind of job, US state and employer.
func (c *Client) JobMarket(ctx context.Context) (*JobMarket, error) {
	var out JobMarket
	text, err := c.callTool(ctx, "get_healthcare_job_market", struct{}{}, &out)
	if err != nil {
		return nil, err
	}
	out.Text = text
	return &out, nil
}

// JobsByState reports open jobs by US state. See StateParams for how the
// parameters choose the answer and StateResult for which fields each answer
// fills.
func (c *Client) JobsByState(ctx context.Context, p StateParams) (*StateResult, error) {
	var out StateResult
	text, err := c.callTool(ctx, "get_healthcare_jobs_by_state", p, &out)
	if err != nil {
		return nil, err
	}
	out.Text = text
	return &out, nil
}

// Employers lists the employers, AI-training programs and job boards whose jobs
// CareJobs.Now reads, most open jobs first.
func (c *Client) Employers(ctx context.Context, p EmployerListParams) (*EmployerList, error) {
	var out EmployerList
	text, err := c.callTool(ctx, "list_healthcare_employers", p, &out)
	if err != nil {
		return nil, err
	}
	out.Text = text
	return &out, nil
}

// Employer returns one employer, AI-training program or job board, by slug (for
// example "cvs-health") or by name. Employers finds the slugs.
func (c *Client) Employer(ctx context.Context, slug string) (*Employer, error) {
	var out Employer
	text, err := c.callTool(ctx, "get_healthcare_employer", map[string]string{"employer": slug}, &out)
	if err != nil {
		return nil, err
	}
	out.Text = text
	return &out, nil
}

// Tools returns the tools the server offers, with their input schemas.
func (c *Client) Tools(ctx context.Context) ([]Tool, error) {
	result, err := c.rpc(ctx, "tools/list", struct{}{}, "")
	if err != nil {
		return nil, err
	}
	var list struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &list); err != nil || list.Tools == nil {
		return nil, &Error{Code: CodeProtocol, Message: "tools/list answered without a tools array", Err: err}
	}
	return list.Tools, nil
}

// Call runs any tool by name with args (a struct, a map or nil) and returns
// the undecoded answer. The typed methods are built on it; use it for a tool
// or a field this module does not know yet.
func (c *Client) Call(ctx context.Context, name string, args any) (*RawResult, error) {
	var data json.RawMessage
	text, err := c.callTool(ctx, name, args, &data)
	if err != nil {
		return nil, err
	}
	return &RawResult{Text: text, Data: data}, nil
}

// toolResult is the result of a tools/call answer.
type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// callTool runs tool with args, decodes its structuredContent into out and
// returns the text summary.
func (c *Client) callTool(ctx context.Context, tool string, args any, out any) (string, error) {
	if args == nil {
		args = struct{}{}
	}
	params := struct {
		Name      string `json:"name"`
		Arguments any    `json:"arguments"`
	}{Name: tool, Arguments: args}

	result, err := c.rpc(ctx, "tools/call", params, tool)
	if err != nil {
		return "", err
	}
	var res toolResult
	if err := json.Unmarshal(result, &res); err != nil {
		return "", &Error{Code: CodeProtocol, Tool: tool, Message: "the tool answered with an unreadable result", Err: err}
	}
	var texts []string
	for _, part := range res.Content {
		if part.Type == "text" {
			texts = append(texts, part.Text)
		}
	}
	text := strings.Join(texts, "\n")
	if res.IsError {
		if text == "" {
			text = tool + " failed"
		}
		return "", &Error{Code: CodeTool, Tool: tool, Message: text}
	}
	trimmed := bytes.TrimSpace(res.StructuredContent)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", &Error{Code: CodeProtocol, Tool: tool, Message: "the tool answered without structured content"}
	}
	if err := json.Unmarshal(trimmed, out); err != nil {
		return "", &Error{Code: CodeProtocol, Tool: tool, Message: "the structured content did not decode", Err: err}
	}
	return text, nil
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// rpc sends one JSON-RPC request and returns the result of its answer.
func (c *Client) rpc(ctx context.Context, method string, params any, tool string) (json.RawMessage, error) {
	if c.timeout > 0 {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, c.timeout)
			defer cancel()
		}
	}
	id := c.nextID.Add(1)
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, &Error{Code: CodeProtocol, Tool: tool, Message: "the arguments cannot be encoded as JSON", Err: err}
	}
	url := c.baseURL + "/api/mcp"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, &Error{Code: CodeNetwork, Tool: tool, Message: "the request cannot be built: " + err.Error(), Err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, transportError(ctx, tool, url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, transportError(ctx, tool, url, err)
	}
	contentType := resp.Header.Get("Content-Type")

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		detail := strings.TrimSpace(string(body))
		if msg, perr := parseMessage(body, contentType, id); perr == nil && msg.Error != nil && msg.Error.Message != "" {
			detail = msg.Error.Message
		} else if len(detail) > 200 {
			detail = detail[:200]
		}
		if detail == "" {
			detail = http.StatusText(resp.StatusCode)
		}
		return nil, &Error{Code: CodeHTTP, Tool: tool, Status: resp.StatusCode, Message: detail}
	}

	msg, err := parseMessage(body, contentType, id)
	if err != nil {
		return nil, &Error{Code: CodeProtocol, Tool: tool, Message: err.Error(), Err: err}
	}
	if msg.Error != nil {
		message := msg.Error.Message
		if message == "" {
			message = "JSON-RPC error"
		}
		return nil, &Error{Code: CodeRPC, Tool: tool, RPCCode: msg.Error.Code, Message: message}
	}
	if len(bytes.TrimSpace(msg.Result)) == 0 {
		return nil, &Error{Code: CodeProtocol, Tool: tool, Message: "the answer held no result"}
	}
	return msg.Result, nil
}

// transportError turns a failure of the HTTP round trip into an *Error.
func transportError(ctx context.Context, tool, url string, err error) *Error {
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		(errors.As(err, &netErr) && netErr.Timeout()):
		return &Error{Code: CodeTimeout, Tool: tool, Message: "no answer from " + url + " in time", Err: err}
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return &Error{Code: CodeCanceled, Tool: tool, Message: "the request to " + url + " was canceled", Err: err}
	default:
		return &Error{Code: CodeNetwork, Tool: tool, Message: "could not reach " + url + ": " + err.Error(), Err: err}
	}
}

// parseMessage reads the JSON-RPC message out of a plain JSON body or a
// text/event-stream body.
func parseMessage(body []byte, contentType string, id int64) (*rpcMessage, error) {
	if !strings.Contains(contentType, "text/event-stream") {
		var msg rpcMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			return nil, errors.New("the server did not answer with JSON: " + err.Error())
		}
		return &msg, nil
	}
	wantID := strconv.FormatInt(id, 10)
	var fallback *rpcMessage
	text := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, event := range strings.Split(text, "\n\n") {
		var data []string
		for _, line := range strings.Split(event, "\n") {
			if rest, ok := strings.CutPrefix(line, "data:"); ok {
				data = append(data, strings.TrimPrefix(rest, " "))
			}
		}
		if len(data) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &msg); err != nil {
			continue
		}
		if string(bytes.TrimSpace(msg.ID)) == wantID {
			return &msg, nil
		}
		if fallback == nil && (len(msg.Result) > 0 || msg.Error != nil) {
			fallback = &msg
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, errors.New("the event stream held no JSON-RPC answer")
}
