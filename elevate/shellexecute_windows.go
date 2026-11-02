package elevate

import (
	"github.com/fosrl/newt/logger"
	"golang.org/x/sys/windows"
)

// ShellExecute elevates and runs a program with the specified arguments
// Uses "runas" verb to show UAC prompt
func ShellExecute(program, arguments, directory string, show int32) error {
	logger.Info("Elevate: ShellExecute called - program: %s, args: %s", program, arguments)
	
	var program16 *uint16
	var arguments16 *uint16
	var directory16 *uint16

	if len(program) > 0 {
		var err error
		program16, err = windows.UTF16PtrFromString(program)
		if err != nil {
			logger.Error("Elevate: Failed to convert program to UTF16: %v", err)
			return err
		}
	}
	if len(arguments) > 0 {
		var err error
		arguments16, err = windows.UTF16PtrFromString(arguments)
		if err != nil {
			logger.Error("Elevate: Failed to convert arguments to UTF16: %v", err)
			return err
		}
	}
	if len(directory) > 0 {
		var err error
		directory16, err = windows.UTF16PtrFromString(directory)
		if err != nil {
			logger.Error("Elevate: Failed to convert directory to UTF16: %v", err)
			return err
		}
	}

	// Use "runas" verb to trigger UAC elevation
	err := windows.ShellExecute(0, windows.StringToUTF16Ptr("runas"), program16, arguments16, directory16, show)
	if err != nil {
		logger.Error("Elevate: ShellExecute failed: %v", err)
		return err
	}
	
	logger.Info("Elevate: ShellExecute succeeded")
	return nil
}

