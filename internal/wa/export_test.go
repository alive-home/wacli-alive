package wa

import (
	"reflect"
	"testing"
	"unsafe"

	"go.mau.fi/whatsmeow"
)

// Hooks for the wa_test package, which drives the real App and the real SDK
// client together.

// RawClient returns the whatsmeow client behind the client an App hands out.
// The App only hands out its read-only guard, so the wrapped *Client is read
// from the guard's unexported "inner" field.
func RawClient(t *testing.T, appClient any) *whatsmeow.Client {
	t.Helper()
	inner := reflect.ValueOf(appClient).Elem().FieldByName("inner")
	if !inner.IsValid() {
		t.Fatalf("%T has no inner client; the App must hand out its guard", appClient)
	}
	c, ok := reflect.NewAt(inner.Type(), unsafe.Pointer(inner.UnsafeAddr())).Elem().Interface().(*Client)
	if !ok {
		t.Fatalf("%T does not wrap a *wa.Client", appClient)
	}
	return c.client
}

func PairedSessionStoreAt(t *testing.T, path string) { newPairedSessionStoreAt(t, path) }
