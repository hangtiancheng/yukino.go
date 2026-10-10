package lsm_tree

import (
	"os"
	"path"

	"github.com/hangtiancheng/yukino.go/components/lsm_tree/filter"
	"github.com/hangtiancheng/yukino.go/components/lsm_tree/memtable"
)

type Config struct {
	Dir      string
	MaxLevel int

	SSTSize          uint64
	SSTNumPerLevel   int
	SSTDataBlockSize int
	SSTFooterSize    int

	Filter              filter.Filter
	MemTableConstructor memtable.MemTableConstructor
}

func NewConfig(dir string, opts ...ConfigOption) (*Config, error) {
	c := Config{
		Dir:           dir,
		SSTFooterSize: 32,
	}

	for _, opt := range opts {
		opt(&c)
	}

	repair(&c)

	return &c, c.check()
}

func (c *Config) check() error {
	if _, err := os.ReadDir(c.Dir); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.MkdirAll(c.Dir, os.ModePerm); err != nil {
			return err
		}
	}

	walDir := path.Join(c.Dir, "walfile")
	if _, err := os.ReadDir(walDir); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err = os.MkdirAll(walDir, os.ModePerm); err != nil {
			return err
		}
	}

	return nil
}

type ConfigOption func(*Config)

func WithMaxLevel(maxLevel int) ConfigOption {
	return func(c *Config) {
		c.MaxLevel = maxLevel
	}
}

func WithSSTSize(sstSize uint64) ConfigOption {
	return func(c *Config) {
		c.SSTSize = sstSize
	}
}

func WithSSTDataBlockSize(sstDataBlockSize int) ConfigOption {
	return func(c *Config) {
		c.SSTDataBlockSize = sstDataBlockSize
	}
}

func WithSSTNumPerLevel(sstNumPerLevel int) ConfigOption {
	return func(c *Config) {
		c.SSTNumPerLevel = sstNumPerLevel
	}
}

func WithFilter(filter filter.Filter) ConfigOption {
	return func(c *Config) {
		c.Filter = filter
	}
}

func WithMemtableConstructor(memtableConstructor memtable.MemTableConstructor) ConfigOption {
	return func(c *Config) {
		c.MemTableConstructor = memtableConstructor
	}
}

func repair(c *Config) {
	if c.MaxLevel <= 1 {
		c.MaxLevel = 7
	}

	if c.SSTSize <= 0 {
		c.SSTSize = 1024 * 1024
	}

	if c.SSTDataBlockSize <= 0 {
		c.SSTDataBlockSize = 16 * 1024
	}

	if c.SSTNumPerLevel <= 0 {
		c.SSTNumPerLevel = 10
	}

	if c.Filter == nil {
		c.Filter, _ = filter.NewBloomFilter(1024)
	}

	if c.MemTableConstructor == nil {
		c.MemTableConstructor = memtable.NewSkiplist
	}
}
