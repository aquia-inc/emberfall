package engine

import (
	"fmt"
	"io"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	TestsPath, UrlPattern, MethodPattern string
	Tests                                []*test `yaml:"tests"`
}

func (c *Config) LoadTests() error {
	var (
		b   []byte
		err error
	)

	fmt.Printf("Reading config from %s\n", c.TestsPath)
	var stat fs.FileInfo

	if c.TestsPath == "-" {
		stat, err = os.Stdin.Stat()
		if err != nil {
			return err
		}

		if stat.Size() < 1 {
			return fmt.Errorf("no config provided")
		}

		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(c.TestsPath)
	}

	if err != nil {
		return err
	}

	return yaml.Unmarshal(b, c)
}
