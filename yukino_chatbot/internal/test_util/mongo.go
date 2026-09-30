package test_util

import (
	"os"
	"strings"
)

const defaultMongoURI = "mongodb://root:pass@localhost:27017/?authSource=admin"

func MongoURI() string {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		return defaultMongoURI
	}
	return uri
}

func IsMongoUnauthorized(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Unauthorized") || strings.Contains(msg, "AuthenticationFailed")
}
