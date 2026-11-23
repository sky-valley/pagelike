package jsglue

import (
	"net/http"
	"testing"

	"github.com/sky-valley/pagelike/internal/hashgate"
	"github.com/sky-valley/pagelike/internal/jsrt"
)

// A busy hashing refusal that escapes a script answers 503 with
// Retry-After: 1 wherever the script ran (R-JS-39d).
func TestBusyRefusalAnswers503(t *testing.T) {
	busy := &jsrt.Failure{Variant: jsrt.VariantThrew, Message: "password hashing is busy (server)" + jsrt.HashBusySuffix}
	for name, err := range map[string]error{
		"compose": composeFailure("j:a", busy),
		"schema":  schemaFailure(busy),
	} {
		ed := hashgate.Refused(err)
		if ed == nil || ed.Status != http.StatusServiceUnavailable || ed.Headers.Get("Retry-After") != "1" {
			t.Errorf("%s: got %v, want the 503 refusal", name, err)
		}
	}
	other := &jsrt.Failure{Variant: jsrt.VariantThrew, Message: "boom"}
	if hashgate.Refused(composeFailure("j:a", other)) != nil || hashgate.Refused(schemaFailure(other)) != nil {
		t.Error("an ordinary throw became a refusal")
	}
}
