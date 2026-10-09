package gox

import (
	"reflect"
	"runtime"
	"strings"
)

func runtimePCName(pc uintptr) string {
	f := runtime.FuncForPC(pc)
	if f == nil {
		return ""
	}
	name := f.Name()
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// depsEqual compares hook dependency lists.
func depsEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !valueEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func valueEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	if ta != tb {
		return false
	}
	if ta.Comparable() {
		if ta.Kind() == reflect.Func {
			return false
		}
		return a == b
	}
	return reflect.DeepEqual(a, b)
}
