package wailsapp

import "sync"

func wireWindowReadyStartup(register func(func()), start func()) {
	var once sync.Once
	register(func() {
		once.Do(start)
	})
}
