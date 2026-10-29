package response

// Business error code constants. HTTP status codes and business error codes are separated:
// the former is for the transport layer, the latter (the code field) is for frontend business branching.
const (
	CodeSuccess         = 0
	CodeBadRequest      = 40000
	CodeUnauthorized    = 40100
	CodeTooManyRequests = 42900
	CodeInternal        = 50000
)
