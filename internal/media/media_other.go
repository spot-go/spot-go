//go:build !linux && (!darwin || !cgo)

package media

func Main(run func()) {
	run()
}

func Listen(func(Command)) {}

func Publish(Info) {}
