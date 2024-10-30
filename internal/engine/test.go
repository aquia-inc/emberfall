package engine

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type test struct {
	Url     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Expect  struct {
		Status  int               `json:"status"`
		Body    *string           `json:"body,omitempty"`
		Headers map[string]string `json:"headers,omitempty"`
	}
}

func (t *test) report(res *http.Response) {
	var errors []string
	result := "PASS"

	if t.Expect.Status != res.StatusCode {
		errors = append(errors, fmt.Sprintf("expected %d got %d", t.Expect.Status, res.StatusCode))
	}

	if t.Expect.Body != nil {
		b, _ := io.ReadAll(res.Body)
		bs := strings.TrimSpace(string(b))
		if *t.Expect.Body != bs {
			errors = append(errors, fmt.Sprintf("expected body %s != %s", *t.Expect.Body, bs))
		}
	}

	if len(t.Expect.Headers) > 0 {
		for expectedHeader, expectedValue := range t.Expect.Headers {
			v, ok := res.Header[http.CanonicalHeaderKey(expectedHeader)]
			value := strings.Join(v, "")
			if !ok {
				errors = append(errors, fmt.Sprintf("expected header %s was missing", expectedHeader))
			} else if expectedValue != value {
				errors = append(errors, fmt.Sprintf("expected header %s:%v != %v", expectedHeader, expectedValue, value))
			}
		}
	}

	if len(errors) > 0 {
		result = "FAIL"
	}

	fmt.Printf("%s : %s\n", result, t.Url)
	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Printf("  %s\n", e)
		}
		code := len(errors)
		if code > 125 {
			code = 125
		}
		os.Exit(code)
	}
}
