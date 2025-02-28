package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var (
	cache map[string]*test = map[string]*test{}
)

// body and test structs are tagged with json because while the config is written in yaml,
// it is not unmarshaled directly into test{}
// json tags here for for the encode/decode process used in newTest to convert interface{}
type body struct {
	Json map[string]interface{} `yaml:"json,omitempty"`
	Text *string                `yaml:"text,omitempty"`
}

type expect struct {
	Status  *int              `yaml:"status,omitempty"`
	Body    *body             `yaml:"body,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

type test struct {
	ID             *string           `yaml:"id"`
	Url            string            `yaml:"url"`
	Method         string            `yaml:"method"`
	Headers        map[string]string `yaml:"headers"`
	FollowRedirect bool              `yaml:"follow"`
	Body           body              `yaml:"body"`
	Expect         expect            `yaml:"expect"`
	Response       interface{}
	errors         []error
	pass           bool
}

func (t *test) bootstrap() {
	if t.Headers == nil {
		t.Headers = map[string]string{}
	}
}

// validate compares the response to the test expectations, collecting any errors, and reporting final status
func (t *test) validate(r *http.Response) bool {
	var err error

	if t.ID != nil {
		cache[*t.ID] = t
	}

	if t.Expect.Status != nil && (*t.Expect.Status != r.StatusCode) {
		t.addError(fmt.Errorf("expected status == %d got %d", *t.Expect.Status, r.StatusCode))
	}

	if t.Expect.Body != nil {
		body, _ := io.ReadAll(r.Body)

		if t.Expect.Body.Text != nil && t.Expect.Body.Json != nil {
			t.addError(errors.New("cannot expect both body.text and body.json"))
		} else if t.Expect.Body.Text != nil {
			s := strings.TrimSpace(string(body))
			t.Response = &s // make it available for referencing in case the test was cached

			if *t.Expect.Body.Text != s {
				t.addError(fmt.Errorf("expected body.text == %s got %s", *t.Expect.Body.Text, s))
			}
		} else if t.Expect.Body.Json != nil {

			var rj map[string]interface{}
			err = json.Unmarshal(body, &rj)

			if err != nil {
				t.addError(err)
			} else {
				t.Response = rj

				t.compare("body.json", t.Expect.Body.Json, rj)
			}
		}
	}

	if len(t.Expect.Headers) > 0 {
		for expectedHeader, expectedValue := range t.Expect.Headers {
			v, ok := r.Header[http.CanonicalHeaderKey(expectedHeader)]
			value := strings.Join(v, "")
			if !ok {
				t.addError(fmt.Errorf("expected header %s was missing", expectedHeader))
			} else if expectedValue != value {
				t.addError(fmt.Errorf("expected header %s:%v got %v", expectedHeader, expectedValue, value))
			}
		}
	}

	t.pass = (len(t.errors) == 0)
	t.report()
	return t.pass
}

func (t *test) addError(e error) *test {
	t.errors = append(t.errors, e)
	return t
}

// report prints test statuses along with any errors
func (t *test) report() {
	var result = map[bool]string{
		true:  "PASS",
		false: "FAIL",
	}

	fmt.Printf("%s : %s %s\n", result[t.pass], t.Method, t.Url)
	if len(t.errors) > 0 {
		for _, e := range t.errors {
			fmt.Printf("  %s\n", e)
		}
	}
}

// compare recursively compares values through the provided maps
func (t *test) compare(prefix string, expect, actual map[string]interface{}) {

	for k, ev := range expect {
		switch expectedValue := ev.(type) {
		case map[string]interface{}: // when the expected value is a map
			switch actualValue := actual[k].(type) {
			case map[string]interface{}: // and the actual value is a map, recurse!, recurse!, recurse!
				t.compare(fmt.Sprintf(prefix+".%s", k), expectedValue, actualValue)
			default: // otherwise when the actual value is not a map it can't possible equate
				t.addError(fmt.Errorf("expected %s.%s == %v got %v", prefix, k, expectedValue, actual[k]))
			}
		// TODO: interpolate string types
		default: // otherwise values other than maps should be compared directly
			if ev != actual[k] {
				t.addError(fmt.Errorf("expected %s.%s == %v got %v", prefix, k, expectedValue, actual[k]))
			}
		}
	}
}
