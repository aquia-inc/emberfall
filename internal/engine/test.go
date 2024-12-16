package engine

import (
	"fmt"
	"net/http"
	"strings"
)

var result = map[bool]string{
	true:  "PASS",
	false: "FAIL",
}

type body struct {
	Json map[string]interface{} `yaml:"json"`
	Text *string                `yaml:"text"`
}

type test struct {
	Url            string            `yaml:"url"`
	Method         string            `yaml:"method"`
	Headers        map[string]string `yaml:"headers"`
	FollowRedirect bool              `yaml:"follow"`
	ReqBody        *body             `yaml:"body,omitempty"`
	Expect         struct {
		Status  int               `yaml:"status"`
		ResBody *body             `yaml:"body,omitempty"`
		Headers map[string]string `yaml:"headers,omitempty"`
	}
	errors []error
	pass   bool
}

func (t *test) validate(res *http.Response) bool {
	if t.Expect.Status != res.StatusCode {
		t.addError(fmt.Errorf("expected status == %d got %d", t.Expect.Status, res.StatusCode))
	}

	// if t.Expect.Body != nil {
	// 	b, _ := io.ReadAll(res.Body)
	// 	bs := strings.TrimSpace(string(b))
	// 	if *t.Expect.Body != bs {
	// 		errors = append(errors, fmt.Sprintf("expected body == %s got %s", *t.Expect.Body, bs))
	// 	}
	// }

	if len(t.Expect.Headers) > 0 {
		for expectedHeader, expectedValue := range t.Expect.Headers {
			v, ok := res.Header[http.CanonicalHeaderKey(expectedHeader)]
			value := strings.Join(v, "")
			if !ok {
				t.addError(fmt.Errorf("expected header %s was missing", expectedHeader))
			} else if expectedValue != value {
				t.addError(fmt.Errorf("expected header %s:%v got %v", expectedHeader, expectedValue, value))
			}
		}
	}

	if len(t.errors) == 0 {
		t.pass = true
	}

	t.report()
	return t.pass
}

func (t *test) addError(e error) *test {
	t.errors = append(t.errors, e)
	return t
}

func (t *test) report() {
	fmt.Printf("%s : %s %s\n", result[t.pass], t.Method, t.Url)
	if len(t.errors) > 0 {
		for _, e := range t.errors {
			fmt.Printf("  %s\n", e)
		}
	}
}
