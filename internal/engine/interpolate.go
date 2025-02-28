package engine

import (
	"bytes"
	"regexp"
	"text/template"
)

var (
	rgx    *regexp.Regexp = regexp.MustCompile(`\{{2}([a-zA-Z\.]*)\}{2}`)
	itpBuf *bytes.Buffer  = new(bytes.Buffer)
)

// interpolate applies text/template to strings that contain {{.path.to.value}} type references
// this allows tests to reference fields in previous tests which is especially useful
// for testing APIs to create a resource then immediately read the resource using the provided ID
func interpolate(s *string) error {
	itpBuf.Truncate(0)
	if rgx.MatchString(*s) {
		t, err := template.New("interpolation").Parse(*s)
		if err != nil {
			return err
		}
		t.Execute(itpBuf, cache)
		*s = itpBuf.String()
	}
	return nil
}
