//go:build !windows

package browser

import (
	"context"
	"encoding/json"
	"errors"
)

type NativeHost struct{}
type NativePage struct{}
type NativeState struct {
	URL, Title, Error                string
	Loading, CanGoBack, CanGoForward bool
}

var errNativeUnsupported = errors.New("embedded browser currently requires Windows WebView2")

func NewNativeHost(func() uintptr, string) *NativeHost { return &NativeHost{} }
func (*NativeHost) NewPage(context.Context, func(NativeState)) (*NativePage, error) {
	return nil, errNativeUnsupported
}
func (*NativePage) Navigate(context.Context, string) error { return errNativeUnsupported }
func (*NativePage) Command(context.Context, string) error  { return errNativeUnsupported }
func (*NativePage) Bounds(context.Context, int32, int32, int32, int32, bool) error {
	return errNativeUnsupported
}
func (*NativePage) Call(context.Context, string, json.RawMessage, func() bool) (json.RawMessage, error) {
	return nil, errNativeUnsupported
}

func (*NativePage) Close() {}
func (*NativePage) CDPTarget(context.Context) (string, error) {
	return "", errNativeUnsupported
}
func (*NativePage) cdpEvent(context.Context, string, func(json.RawMessage)) (func(), error) {
	return nil, errNativeUnsupported
}
func (*NativePage) DispatchCDP(context.Context, string, func() bool, func() error) error {
	return errNativeUnsupported
}
