package engine

import (
	"fmt"
	"net/http"
)

func Run(cfg *config) {
	var (
		req *http.Request
		res *http.Response
		err error
	)

	for _, test := range cfg.Tests {

		req, err = http.NewRequest(test.Method, test.Url, nil)

		if err != nil {
			fmt.Println(err)
			continue
		}

		for k, v := range test.Headers {
			req.Header.Set(k, v)
		}

		res, err = http.DefaultClient.Do(req)
		if err != nil {
			fmt.Println(err)
			continue
		}

		test.report(res)
	}
}
