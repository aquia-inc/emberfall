package engine

import (
	"fmt"
	"net/http"
)

func Run(cfg *config) (success bool) {
	var (
		client   *http.Client = &http.Client{}
		req      *http.Request
		res      *http.Response
		err      error
		failures int
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

		if test.FollowRedirect {
			client.CheckRedirect = nil
		} else {
			client.CheckRedirect = noRedirect
		}

		res, err = client.Do(req)
		if err != nil {
			fmt.Println(err)
			continue
		}

		success = test.report(res)
		if !success {
			failures++
		}
	}

	fmt.Printf("\n Ran %d tests with %d failures\n", len(cfg.Tests), failures)
	return
}

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
