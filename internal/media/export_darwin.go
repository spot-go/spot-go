//go:build darwin && cgo

package media

import "C"

//export mediaCommand
func mediaCommand(code C.int) {
	dispatch(Command(code))
}
