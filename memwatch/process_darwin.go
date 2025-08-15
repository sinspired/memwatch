//go:build darwin

package memwatch

import (
    "fmt"
    "os/exec"
    "strconv"
    "strings"
)

// findPIDByName 查找指定名称的进程 PID（仅适用于 macOS）
func findPIDByName(name string) (int, error) {
    // 使用 pgrep 获取进程 PID
    cmd := exec.Command("pgrep", name)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return 0, fmt.Errorf("failed to find process %q: %s", name, err)
    }

    pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
    if err != nil {
        return 0, fmt.Errorf("failed to parse PID: %s", output)
    }
    return pid, nil
}

// getMemoryBytes 获取指定 PID 的内存使用情况（仅适用于 macOS）
func getMemoryBytes(pid int) (uint64, error) {
    cmd := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid))
    output, err := cmd.CombinedOutput()
    if err != nil {
        return 0, fmt.Errorf("failed to get memory for PID %d: %s", pid, err)
    }
    rss, err := strconv.Atoi(strings.TrimSpace(string(output)))
    if err != nil {
        return 0, fmt.Errorf("failed to parse memory usage: %s", output)
    }
    return uint64(rss) * 1024, nil // 转换为字节
}
