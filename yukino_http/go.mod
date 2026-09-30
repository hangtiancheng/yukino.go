module github.com/hangtiancheng/yukino.go/yukino_http

go 1.26.0

replace (
	github.com/hangtiancheng/yukino.go/yukino_cache => ../yukino_cache
	// github.com/hangtiancheng/yukino.go/yukino_http => ../yukino_http
	github.com/hangtiancheng/yukino.go/yukino_orm => ../yukino_orm
	github.com/hangtiancheng/yukino.go/yukino_rpc => ../yukino_rpc
)
