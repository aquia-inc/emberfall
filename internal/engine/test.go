package engine

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

type test struct {
	Url            string            `yaml:"url"`
	Method         string            `yaml:"method"`
	Headers        map[string]string `yaml:"headers"`
	FollowRedirect bool              `yaml:"follow"`
	Expect         struct {
		Status  int               `yaml:"status"`
		Body    *string           `yaml:"body,omitempty"`
		Headers map[string]string `yaml:"headers,omitempty"`
	}
}

func (t *test) report(res *http.Response) (success bool) {
	errors := []string{}

	result := map[bool]string{
		true:  "PASS",
		false: "FAIL",
	}

	if t.Expect.Status != res.StatusCode {
		errors = append(errors, fmt.Sprintf("expected status == %d got %d", t.Expect.Status, res.StatusCode))
	}

	if t.Expect.Body != nil {
		b, _ := io.ReadAll(res.Body)
		bs := strings.TrimSpace(string(b))
		if *t.Expect.Body != bs {
			errors = append(errors, fmt.Sprintf("expected body == %s got %s", *t.Expect.Body, bs))
		}
	}

	if len(t.Expect.Headers) > 0 {
		for expectedHeader, expectedValue := range t.Expect.Headers {
			v, ok := res.Header[http.CanonicalHeaderKey(expectedHeader)]
			value := strings.Join(v, "")
			if !ok {
				errors = append(errors, fmt.Sprintf("expected header %s was missing", expectedHeader))
			} else if expectedValue != value {
				errors = append(errors, fmt.Sprintf("expected header %s:%v got %v", expectedHeader, expectedValue, value))
			}
		}
	}

	if len(errors) == 0 {
		success = true
	}

	fmt.Printf("%s : %s %s\n", result[success], t.Method, t.Url)
	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Printf("  %s\n", e)
		}
	}

	return
}
