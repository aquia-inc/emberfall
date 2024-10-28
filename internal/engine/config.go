package engine

import (
	"fmt"
	"io"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"
)

type config struct {
	Run   string  `json:"run"`
	Tests []*test `json:"tests"`
}

func LoadConfig(configPath string) (*config, error) {
	var (
		b   []byte
		err error
	)

	fmt.Printf("Reading config from %s\n", configPath)
	var stat fs.FileInfo

	if configPath == "-" {
		stat, err = os.Stdin.Stat()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		if stat.Size() < 1 {
			fmt.Println("no config provided")
			os.Exit(1)
		}

		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(configPath)
	}

	if err != nil {
		return nil, err
	}

	conf := &config{}
	err = yaml.Unmarshal(b, conf)
	return conf, err
}
