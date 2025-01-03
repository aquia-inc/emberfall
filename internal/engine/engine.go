package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

func Run(cfg *config) (success bool) {
	var (
		client   *http.Client = &http.Client{}
		req      *http.Request
		res      *http.Response
		failures int
	)

	// TODO: refactor this loop into Tests.run() by making config.Tests type Tests as []*test
	for _, test := range cfg.Tests {
		var (
			err error
			b   []byte
		)

		test.bootstrap()

		buf := new(bytes.Buffer)

		if test.ReqBody.Json != nil && test.ReqBody.Text != nil {
			test.addError(errors.New("may define body.json or body.text but not both"))
			continue
		}

		if test.ReqBody.Json != nil {
			b, err = json.Marshal(test.ReqBody.Json)
			if err != nil {
				test.addError(err)
				continue
			}

			if _, ok := test.Headers["Content-Type"]; !ok {
				test.Headers["Content-Type"] = "application/json"
			}
		}

		if test.ReqBody.Text != nil {
			b = []byte(*test.ReqBody.Text)
			if _, ok := test.Headers["Content-Type"]; !ok {
				test.Headers["Content-Type"] = "text/plain"
			}
		}

		buf = bytes.NewBuffer(b)

		req, err = http.NewRequest(test.Method, test.Url, buf)

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
	}

	if failures == 0 {
		success = true
	}

	fmt.Printf("\nRan %d tests with %d failures\n", len(cfg.Tests), failures)
	return
}

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
