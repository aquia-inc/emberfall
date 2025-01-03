package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var result = map[bool]string{
	true:  "PASS",
	false: "FAIL",
}

type body struct {
	Json map[string]interface{} `yaml:"json,omitempty"`
	Text *string                `yaml:"text,omitempty"`
}

type test struct {
	Url            string            `yaml:"url"`
	Method         string            `yaml:"method"`
	Headers        map[string]string `yaml:"headers"`
	FollowRedirect bool              `yaml:"follow"`
	ReqBody        body              `yaml:"body"`
	Expect         struct {
		Status  *int              `yaml:"status,omitempty"`
		ResBody *body             `yaml:"body,omitempty"`
		Headers map[string]string `yaml:"headers,omitempty"`
	}
	errors []error
	pass   bool
}

func (t *test) bootstrap() {
	if t.Headers == nil {
		t.Headers = map[string]string{}
	}
}

func (t *test) validate(res *http.Response) bool {
	var err error

	if t.Expect.Status != nil {
		if *t.Expect.Status != res.StatusCode {
			t.addError(fmt.Errorf("expected status == %d got %d", *t.Expect.Status, res.StatusCode))
		}
	}

	if t.Expect.ResBody != nil {
		bodyBytes, _ := io.ReadAll(res.Body)

		if t.Expect.ResBody.Text != nil && t.Expect.ResBody.Json != nil {
			t.addError(errors.New("may expect body.text or body.json but not both"))
		} else if t.Expect.ResBody.Text != nil {
			bs := strings.TrimSpace(string(bodyBytes))

			if *t.Expect.ResBody.Text != bs {
				t.addError(fmt.Errorf("expected body.text == %s got %s", *t.Expect.ResBody.Text, bs))
			}
		} else if t.Expect.ResBody.Json != nil {
			var resJson map[string]interface{}
			err = json.Unmarshal(bodyBytes, &resJson)

			if err != nil {
				t.addError(err)
			} else {
				// compare returned json to expect.body.json recursively
				t.compare("body.json", t.Expect.ResBody.Json, resJson)
			}
		}
	}

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

func (t *test) compare(prefix string, a, b map[string]interface{}) {
	for k, v := range a {
		switch av := v.(type) {
		case map[string]interface{}:
			switch bv := b[k].(type) {
			case map[string]interface{}:
				t.compare(fmt.Sprintf(prefix+".%s", k), av, bv)
			default:
				t.addError(fmt.Errorf("expected %s.%s == %v got %v", prefix, k, v, b[k]))
			}

		default:
			if b[k] != v {
				t.addError(fmt.Errorf("expected %s.%s == %v got %v", prefix, k, v, b[k]))
			}
		}
	}
}
