package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
)

func Run(cfg *Config) error {
	// reduce memory allocations by reusing as many
	var (
		client               = &http.Client{}
		req                  *http.Request
		ran, skipped, failed int
		reqBuf               = new(bytes.Buffer)
		includedURLs         *regexp.Regexp
		includedMethods      *regexp.Regexp
		err                  error
	)

	err = cfg.LoadTests()
	if err != nil {
		return err
	}

	if cfg.UrlPattern != "" {
		includedURLs, err = regexp.Compile(cfg.UrlPattern)
		if err != nil {
			return err
		}
	}

	if cfg.MethodPattern != "" {
		includedMethods, err = regexp.Compile(cfg.MethodPattern)
		if err != nil {
			return err
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

		if includedURLs != nil && !includedURLs.MatchString(test.Url) {
			skipped++
			continue
		}

		if includedMethods != nil && !includedMethods.MatchString(test.Method) { // '^P'
			skipped++
			continue
		}

		err = interpolate(&test.Url)
		if err != nil {
			test.addError(err)
			continue
		}

		bodyTypes := 0
		if test.Body.Json != nil {
			bodyTypes++
		}
		if test.Body.Text != nil {
			bodyTypes++
		}
		if test.Body.Multipart != nil {
			bodyTypes++
		}
		if bodyTypes > 1 {
			test.addError(errors.New("body may define only one of json, text, or multipart"))
			continue
		}

		var contentType string

		if test.Body.Json != nil {
			body, err = json.Marshal(test.Body.Json)
			if err != nil {
				test.addError(err)
				continue
			}
			reqBuf.Write(body)
			contentType = "application/json"
		}

		if test.Body.Text != nil {
			reqBuf.WriteString(*test.Body.Text)
			contentType = "text/plain"
		}

		if test.Body.Multipart != nil {
			contentType, err = writeMultipart(reqBuf, test.Body.Multipart)
			if err != nil {
				test.addError(err)
			}
		}

		if contentType != "" {
			if _, ok := test.Headers["Content-Type"]; !ok {
				test.Headers["Content-Type"] = contentType
			}
		}

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

	if failed > 0 {
		return errors.New("tests failed")
	}

	return nil
}

func noRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}
