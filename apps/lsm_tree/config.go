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

package lsm_tree

import (
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/filter"
	"github.com/hangtiancheng/yukino.go/apps/lsm_tree/memtable"
)

// Config aggregates lsm tree configuration.
type Config struct {
	Dir      string // directory for sst files
	MaxLevel int    // total number of levels in the lsm tree

	// SST-related settings
	SSTSize          uint64 // size of each sst table; default 4M
	SSTNumPerLevel   int    // number of sstables per level; default 10
	SSTDataBlockSize int    // block size within an sst table; default 16KB
	SSTFooterSize    int    // footer size in an sst table; fixed 32B

	Filter              filter.Filter                // filter; defaults to bloom filter
	MemTableConstructor memtable.MemTableConstructor // memtable constructor; defaults to skiplist
}

// NewConfig builds a Config. It also ensures the sst and wal directories exist.
func NewConfig(dir string, opts ...ConfigOption) (*Config, error) {
	c := Config{
		Dir:           dir, // directory for sst files
		SSTFooterSize: 32,  // 4 uvarints = 32 bytes
	}

	// Apply options.
	for _, opt := range opts {
		opt(&c)
	}

	// Fill defaults.
	repair(&c)

	return &c, c.check() // validate and create directories if missing
}

// check validates the config and creates the sst and wal directories if missing.
func (c *Config) check() error {
	// Ensure the sstable directory exists.
	if _, err := os.ReadDir(c.Dir); err != nil {
		_, ok := err.(*fs.PathError)
		if !ok || !strings.HasSuffix(err.Error(), "no such file or directory") {
			return err
		}
		if err = os.Mkdir(c.Dir, os.ModePerm); err != nil {
			return err
		}
	}

	// Ensure the wal directory exists.
	walDir := path.Join(c.Dir, "walfile")
	if _, err := os.ReadDir(walDir); err != nil {
		_, ok := err.(*fs.PathError)
		if !ok || !strings.HasSuffix(err.Error(), "no such file or directory") {
			return err
		}
		if err = os.Mkdir(walDir, os.ModePerm); err != nil {
			return err
		}
	}

	return nil
}

// ConfigOption mutates Config.
type ConfigOption func(*Config)

// WithMaxLevel sets the maximum number of levels. Default is 7.
func WithMaxLevel(maxLevel int) ConfigOption {
	return func(c *Config) {
		c.MaxLevel = maxLevel
	}
}

// WithSSTSize sets the sstable file size for level 0 in bytes. Default is 1MB.
// Each deeper level multiplies the size limit by 10.
func WithSSTSize(sstSize uint64) ConfigOption {
	return func(c *Config) {
		c.SSTSize = sstSize
	}
}

// WithSSTDataBlockSize sets the block size within an sstable. Default is 16KB.
func WithSSTDataBlockSize(sstDataBlockSize int) ConfigOption {
	return func(c *Config) {
		c.SSTDataBlockSize = sstDataBlockSize
	}
}

// WithSSTNumPerLevel sets the expected max number of sstables per level. Default is 10.
func WithSSTNumPerLevel(sstNumPerLevel int) ConfigOption {
	return func(c *Config) {
		c.SSTNumPerLevel = sstNumPerLevel
	}
}

// WithFilter injects a filter implementation. Defaults to the built-in bloom filter.
func WithFilter(filter filter.Filter) ConfigOption {
	return func(c *Config) {
		c.Filter = filter
	}
}

// WithMemtableConstructor injects a memtable constructor. Defaults to the built-in skiplist.
func WithMemtableConstructor(memtableConstructor memtable.MemTableConstructor) ConfigOption {
	return func(c *Config) {
		c.MemTableConstructor = memtableConstructor
	}
}

func repair(c *Config) {
	// Default to 7 levels.
	if c.MaxLevel <= 1 {
		c.MaxLevel = 7
	}

	// Default level-0 sstable size is 1MB.
	// Each deeper level multiplies the size limit by 10.
	if c.SSTSize <= 0 {
		c.SSTSize = 1024 * 1024
	}

	// Default block size is 16KB.
	if c.SSTDataBlockSize <= 0 {
		c.SSTDataBlockSize = 16 * 1024 // 16KB
	}

	// Default 10 sstables per level.
	if c.SSTNumPerLevel <= 0 {
		c.SSTNumPerLevel = 10
	}

	// Default to the built-in bloom filter.
	if c.Filter == nil {
		c.Filter, _ = filter.NewBloomFilter(1024)
	}

	// Default to the built-in skiplist.
	if c.MemTableConstructor == nil {
		c.MemTableConstructor = memtable.NewSkiplist
	}
}
