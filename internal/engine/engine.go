package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
)

func Run(cfg *config, include, exclude string) bool {
	// reduce memory allocations by reusing things that dont need to be cached
	var (
		client               = &http.Client{}
		req                  *http.Request
		ran, skipped, failed int
		reqBuf               = new(bytes.Buffer)
		included, excluded   *regexp.Regexp
		err                  error
	)

	// compile include/exclude strings into regular expressions
	// return false if either fails

	if exclude != "" {
		excluded, err = regexp.Compile(exclude)
		if err != nil {
			fmt.Println(err)
			return false
		}
	}

	if include != "" {
		included, err = regexp.Compile(include)
		if err != nil {
			fmt.Println(err)
			return false
		}
	}

	// TODO: refactor this loop into Tests.run() by making config.Tests type Tests as []*test
	for _, test := range cfg.Tests {
		var (
			err  error
			body []byte
			res  *http.Response
		)

		reqBuf.Truncate(0)
		test.bootstrap()

		// filter excluded/included
		if (excluded != nil && excluded.MatchString(*test.ID)) || (included != nil && !included.MatchString(*test.ID)) {
			skipped++
			continue
		}

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
			failed++
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
			failed++
			continue
		}

		if !test.validate(res) {
			failed++
		}

		ran++
	} // end for

	fmt.Printf("\n    Ran: %d\n Failed: %d\nSkipped: %d\n", ran, failed, skipped)
	return (failed == 0)
}

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
