// Copyright (c) 2026 hangtiancheng
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package conf

// RedisConfig holds cache configuration.
type RedisConfig struct {
	Network            string `yaml:"network"`
	Address            string `yaml:"address"`
	Password           string `yaml:"password"`
	MaxIdle            int    `yaml:"maxIdle"`
	IdleTimeoutSeconds int    `yaml:"idleTimeout"`
	// Maximum number of active connections in the pool.
	MaxActive int `yaml:"maxActive"`
	// Whether new requests wait or fail immediately when the pool is full.
	Wait bool `yaml:"wait"`
}

type RedisConfigProvider struct {
	conf *RedisConfig
}

func NewRedisConfigProvider(conf *RedisConfig) *RedisConfigProvider {
	return &RedisConfigProvider{
		conf: conf,
	}
}

func (r *RedisConfigProvider) Get() *RedisConfig {
	return r.conf
}

var defaultRedisConfProvider *RedisConfigProvider

func DefaultRedisConfigProvider() *RedisConfigProvider {
	return defaultRedisConfProvider
}
