//go:build windows

package captcha

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	browserJob     windows.Handle
	browserJobOnce sync.Once
	browserJobErr  error
)

func hideProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func attachProcess(command *exec.Cmd) error {
	browserJobOnce.Do(func() {
		browserJob, browserJobErr = windows.CreateJobObject(nil, nil)
		if browserJobErr != nil {
			return
		}
		information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		_, browserJobErr = windows.SetInformationJobObject(
			browserJob,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&information)),
			uint32(unsafe.Sizeof(information)),
		)
	})
	if browserJobErr != nil {
		return fmt.Errorf("create browser job object: %w", browserJobErr)
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(command.Process.Pid))
	if err != nil {
		return fmt.Errorf("open browser process: %w", err)
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(browserJob, process); err != nil {
		return fmt.Errorf("assign browser to job object: %w", err)
	}
	return nil
}

func killProcessTree(pid int) {
	command := exec.Command("taskkill", "/PID", stringPID(pid), "/T", "/F")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = command.Run()
}
