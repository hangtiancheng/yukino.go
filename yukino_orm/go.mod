module github.com/hangtiancheng/yukino.go/yukino_orm

go 1.26.0

require go.mongodb.org/mongo-driver/v2 v2.9.1

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/klauspost/compress v1.19.2 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.2.0 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace (
	github.com/hangtiancheng/yukino.go/yukino_cache => ../yukino_cache
	github.com/hangtiancheng/yukino.go/yukino_http => ../yukino_http
	// github.com/hangtiancheng/yukino.go/yukino_orm => ../yukino_orm
	github.com/hangtiancheng/yukino.go/yukino_rpc => ../yukino_rpc
)
