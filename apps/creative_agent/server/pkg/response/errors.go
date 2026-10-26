package response

// 业务错误码常量。HTTP 状态码与业务错误码分离：前者用于传输层，
// 后者（code 字段）用于前端业务分支判断。
const (
	CodeSuccess         = 0
	CodeBadRequest      = 40000
	CodeUnauthorized    = 40100
	CodeTooManyRequests = 42900
	CodeInternal        = 50000
)
