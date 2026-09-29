package jsrt

import (
	"strings"
	"testing"
)

// TestStackShowsOnlyBindingFrames: frames of the (stripped) prelude and the
// driver never appear; the binding's frames keep eval:line:column.
func TestStackShowsOnlyBindingFrames(t *testing.T) {
	f := expectFailure(t, read("export default function () {\n  const d = new DOMParser().parseFromString('<p>x</p>', 'text/html');\n  return [1].map(() => d.querySelector('p').setAttribute('a b', 1));\n}", nil), VariantThrew)
	if !strings.HasPrefix(f.Message, "InvalidCharacterError: ") {
		t.Errorf("message %q", f.Message)
	}
	lines := strings.Split(f.Stack, "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "(eval:3:") || !strings.Contains(lines[1], "map (native)") || !strings.Contains(lines[2], "at default (eval:3:") {
		t.Errorf("stack:\n%s", f.Stack)
	}
}
