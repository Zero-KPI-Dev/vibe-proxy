//go:build windows

package app

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	replaceFileW         = kernel32.NewProc("ReplaceFileW")
	replaceFileNoOptions uintptr
)

// replacePreferenceFile uses Rename for the initial write and ReplaceFileW for
// an existing target. ReplaceFileW performs the replacement as one Windows
// operation while preserving the replaced file's identity and attributes.
func replacePreferenceFile(temporaryPath, path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return os.Rename(temporaryPath, path)
	} else if err != nil {
		return fmt.Errorf("stat existing preferences: %w", err)
	}

	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("encode preferences path: %w", err)
	}
	temporary, err := windows.UTF16PtrFromString(temporaryPath)
	if err != nil {
		return fmt.Errorf("encode temporary preferences path: %w", err)
	}

	result, _, callErr := replaceFileW.Call(
		uintptr(unsafe.Pointer(target)),
		uintptr(unsafe.Pointer(temporary)),
		replaceFileNoOptions,
		replaceFileNoOptions,
		replaceFileNoOptions,
		replaceFileNoOptions,
	)
	if result != 0 {
		return nil
	}
	if callErr == windows.ERROR_SUCCESS {
		return errors.New("ReplaceFileW failed without a Windows error")
	}
	return fmt.Errorf("ReplaceFileW: %w", callErr)
}
