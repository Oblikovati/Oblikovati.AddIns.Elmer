// SPDX-License-Identifier: GPL-2.0-only

package main

/*
#cgo CFLAGS: -I${SRCDIR}/include -DOBK_BUILDING_ADDIN
#include <stdlib.h>
#include <stdint.h>
#include "oblikovati_addin.h"

// Trampolines: cgo cannot call a Go-held C function pointer directly, so invoke the
// host's callbacks from C. (These are definitions, so they live here and not in
// export.go, whose //export'd functions forbid preamble definitions.)
static int  host_call(ObkHostCall c, const char* m, const uint8_t* req, int n, uint8_t** resp, int* rl) { return c(m, req, n, resp, rl); }
static void host_free(ObkHostFree f, uint8_t* p) { f(p); }
*/
import "C"
import (
	"errors"
	"unsafe"
)

// cgoHostCaller implements elmer.HostCaller over the C-ABI host callbacks stored on
// Activate. Call blocks until the host runs the request on its session goroutine.
type cgoHostCaller struct{}

// Call forwards a JSON method request to the host and returns its JSON reply. A
// non-OK status returns the host's error message; the response buffer (host-owned)
// is always released via ObkHostFree.
func (cgoHostCaller) Call(method string, req []byte) ([]byte, error) {
	mu.Lock()
	call, freeFn := hostCall, hostFree
	mu.Unlock()
	if call == nil {
		return nil, errors.New("elmer: host not connected")
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var reqPtr *C.uint8_t
	if len(req) > 0 {
		reqPtr = (*C.uint8_t)(unsafe.Pointer(&req[0]))
	}
	var resp *C.uint8_t
	var respLen C.int
	rc := C.host_call(call, cMethod, reqPtr, C.int(len(req)), &resp, &respLen)
	if resp == nil {
		if rc == C.OBK_OK {
			return []byte{}, nil
		}
		return nil, errors.New("elmer: host call failed")
	}
	out := C.GoBytes(unsafe.Pointer(resp), respLen)
	C.host_free(freeFn, resp)
	if rc != C.OBK_OK {
		return nil, errors.New(string(out)) // host-formatted error message
	}
	return out, nil
}
