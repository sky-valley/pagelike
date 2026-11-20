package jsrt

import (
	"testing"

	"modernc.org/libc"
	lib "modernc.org/libquickjs"
	"modernc.org/quickjs"
)

// vmContext reaches into quickjs.VM for its JSContext; a wrapper upgrade that
// moves the field must fail here, not as a nil dereference in a worker.
func TestVMContextIsTheVMsLiveContext(t *testing.T) {
	vm, err := quickjs.NewVM()
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	ctx := vmContext(vm)
	if ctx == 0 {
		t.Fatal("vmContext returned no context")
	}
	tls := libc.NewTLS()
	defer tls.Close()
	if lib.XJS_GetRuntime(tls, ctx) == 0 {
		t.Fatal("vmContext's pointer is not a JSContext with a runtime")
	}
}
