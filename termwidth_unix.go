//go:build unix

package main

import (
	"syscall"
	"unsafe"
)

// ttyWidth は fd の端末桁数を ioctl(TIOCGWINSZ) で取得する。端末でない (パイプ等) /
// 取得失敗時は 0。x/term などに依存せず syscall で直接引く (依存を増やさない方針)。
func ttyWidth(fd uintptr) int {
	var ws struct {
		Row, Col, Xpixel, Ypixel uint16
	}
	_, _, errno := syscall.Syscall(
		syscall.SYS_IOCTL,
		fd,
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&ws)),
	)
	if errno != 0 {
		return 0
	}
	return int(ws.Col)
}
