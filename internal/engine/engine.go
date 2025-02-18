package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func Run(cfg *config) (success bool) {
	// reduce memory allocations by reusing things that dont need to be cached
	var (
		client   *http.Client = &http.Client{}
		req      *http.Request
		failures int
		reqBuf   *bytes.Buffer = new(bytes.Buffer)
	)

	// TODO: refactor this loop into Tests.run() by making config.Tests type Tests as []*test
	for _, test := range cfg.Tests {
		var (
			err  error
			body []byte
			res  *http.Response
		)

		reqBuf.Truncate(0)
		test.bootstrap()
		err = interpolate(&test.Url)
		if err != nil {
			test.addError(err)
			continue
		}

		if test.Body.Json != nil && test.Body.Text != nil {
			test.addError(errors.New("may define body.json or body.text but not both"))
			continue
		}

		if test.Body.Json != nil {
			body, err = json.Marshal(test.Body.Json)
			if err != nil {
				test.addError(err)
				continue
			}

			if _, ok := test.Headers["Content-Type"]; !ok {
				test.Headers["Content-Type"] = "application/json"
			}
		}

		if test.Body.Text != nil {
			body = []byte(*test.Body.Text)
			if _, ok := test.Headers["Content-Type"]; !ok {
				test.Headers["Content-Type"] = "text/plain"
			}
		}

		reqBuf.Write(body)

		req, err = http.NewRequest(test.Method, test.Url, reqBuf)

		if err != nil {
			test.addError(err)
		}

		// exit early if test already has errors
		if len(test.errors) > 0 {
			test.report()
			failures++
			continue
		}

		for k, v := range test.Headers {
			err = interpolate(&v)
			if err != nil {
				test.addError(err)
				continue
			}
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
			failures++
			continue
		}

		if !test.validate(res) {
			failures++
		}
	} // end for

	success = (failures == 0)

	fmt.Printf("\nRan %d tests with %d failures\n", len(cfg.Tests), failures)
	return
}

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
