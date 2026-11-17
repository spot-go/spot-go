//go:build !linux && (!darwin || !cgo)

package media

import "context"

func Main(run func()) {
	run()
}

func Listen(func(Command)) {}

func Publish(Info) {}

func Connected(context.Context) (<-chan struct{}, error) {
	return nil, ErrUnsupported
}
