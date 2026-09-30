package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
)

type AppConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type MongoConfig struct {
	URI      string `json:"uri"`
	Database string `json:"database"`
}

type CacheConfig struct {
	MaxBytes   int64 `json:"maxBytes"`
	Expiration int   `json:"expiration"`
}

type StaticConfig struct {
	AvatarPath string `json:"avatarPath"`
	FilePath   string `json:"filePath"`
	ChunkPath  string `json:"chunkPath"`
}

type AuthConfig struct {
	JwtSecret        string `json:"jwtSecret"`
	TokenExpireHours int    `json:"tokenExpireHours"`
}

type Config struct {
	App    AppConfig    `json:"app"`
	Mongo  MongoConfig  `json:"mongo"`
	Cache  CacheConfig  `json:"cache"`
	Static StaticConfig `json:"static"`
	Auth   AuthConfig   `json:"auth"`
}

var conf *Config

func Load(path string) *Config {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read config: %v", err)
	}
	conf = &Config{}
	if err := json.Unmarshal(data, conf); err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}
	if conf.Auth.JwtSecret == "" {
		// Tokens signed with an ephemeral secret become invalid on restart.
		buf := make([]byte, 32)
		_, _ = rand.Read(buf)
		conf.Auth.JwtSecret = hex.EncodeToString(buf)
		log.Println("auth.jwtSecret not configured; using an ephemeral secret (tokens expire on restart)")
	}
	if conf.Auth.TokenExpireHours <= 0 {
		conf.Auth.TokenExpireHours = 14 * 24
	}
	if conf.Static.ChunkPath == "" {
		conf.Static.ChunkPath = "./static/chunks"
	}
	return conf
}

func Get() *Config {
	if conf == nil {
		log.Fatal("config not loaded")
	}
	return conf
}
