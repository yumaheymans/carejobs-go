package carejobs

import "fmt"

// ErrorCode says where an Error came from.
type ErrorCode string

const (
	// CodeTool: the server ran the tool and answered with an error, for example
	// an unknown employer or an invalid kind. Message holds the server's text.
	CodeTool ErrorCode = "tool"
	// CodeRPC: a JSON-RPC error (unknown tool, malformed request). RPCCode is set.
	CodeRPC ErrorCode = "rpc"
	// CodeHTTP: a non-2xx HTTP status. Status is set.
	CodeHTTP ErrorCode = "http"
	// CodeNetwork: the request never completed (DNS, connection reset, and so on).
	CodeNetwork ErrorCode = "network"
	// CodeTimeout: no answer before the context deadline or the HTTP client timeout.
	CodeTimeout ErrorCode = "timeout"
	// CodeCanceled: the context was canceled before the answer arrived.
	CodeCanceled ErrorCode = "canceled"
	// CodeProtocol: the answer was not the shape the MCP protocol promises.
	CodeProtocol ErrorCode = "protocol"
)

// Error is the error type every Client method returns. Use errors.As to read it:
//
//	var apiErr *carejobs.Error
//	if errors.As(err, &apiErr) && apiErr.Code == carejobs.CodeTool {
//		fmt.Println("the server said:", apiErr.Message)
//	}
//
// Unwrap exposes the underlying cause, so errors.Is(err, context.DeadlineExceeded)
// works for a CodeTimeout error.
type Error struct {
	// Code says where the error came from.
	Code ErrorCode
	// Message is the server's text for a CodeTool, CodeRPC or CodeHTTP error,
	// and a description of what went wrong otherwise.
	Message string
	// Tool is the tool that was called, when there was one.
	Tool string
	// Status is the HTTP status of a CodeHTTP error.
	Status int
	// RPCCode is the JSON-RPC error code of a CodeRPC error.
	RPCCode int
	// Err is the underlying cause (a network or decoding error), if any.
	Err error
}

// Error implements the error interface.
func (e *Error) Error() string {
	prefix := "carejobs"
	if e.Tool != "" {
		prefix += " " + e.Tool
	}
	switch e.Code {
	case CodeHTTP:
		return fmt.Sprintf("%s: HTTP %d: %s", prefix, e.Status, e.Message)
	case CodeRPC:
		return fmt.Sprintf("%s: rpc error %d: %s", prefix, e.RPCCode, e.Message)
	default:
		return fmt.Sprintf("%s: %s: %s", prefix, e.Code, e.Message)
	}
}

// Unwrap returns the underlying cause, if any.
func (e *Error) Unwrap() error { return e.Err }
