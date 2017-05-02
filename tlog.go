package tlog

import (
	"fmt"
	"runtime"
)

func Infof(format string, args ...interface{}) {
	_, file, line, _ := runtime.Caller(3)
	fmt.Println(file, line)
}
