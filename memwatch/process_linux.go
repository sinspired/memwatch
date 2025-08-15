//go:build linux || darwin

package memwatch

import (
    "bufio"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "strings"
)

// findPIDByName 查找指定名称的进程 PID
func findPIDByName(name string) (int, error) {
    // 在 Linux 下，优先匹配 /proc/<pid>/comm 的完整程序名
    if isLinux() {
        ents, err := os.ReadDir("/proc")
        if err != nil {
            return 0, err
        }
        for _, e := range ents {
            if !e.IsDir() {
                continue
            }
            pid, err := strconv.Atoi(e.Name())
            if err != nil || pid <= 0 {
                continue
            }
            commPath := filepath.Join("/proc", e.Name(), "comm")
            b, err := os.ReadFile(commPath)
            if err != nil {
                continue
            }
            comm := strings.TrimSpace(string(b))
            if comm == name {
                return pid, nil
            }
        }
    } else if isDarwin() {
        // 在 macOS 下，使用 pgrep 查找 PID
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
    return 0, fmt.Errorf("unsupported platform")
}

// getMemoryBytes 获取指定 PID 的内存使用情况
func getMemoryBytes(pid int) (uint64, error) {
    if isLinux() {
        if v, err := privateFromSmapsRollup(pid); err == nil {
            return v, nil
        } else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
            return 0, err
        }
        return privateFromSmaps(pid)
    } else if isDarwin() {
        return getMemoryBytesMac(pid)
    }
    return 0, fmt.Errorf("unsupported platform")
}

// privateFromSmapsRollup 获取 Linux 上的内存统计（Private_Clean + Private_Dirty）
func privateFromSmapsRollup(pid int) (uint64, error) {
    path := filepath.Join("/proc", strconv.Itoa(pid), "smaps_rollup")
    f, err := os.Open(path)
    if err != nil {
        return 0, err
    }
    defer f.Close()

    var kb uint64
    sc := bufio.NewScanner(f)
    for sc.Scan() {
        line := sc.Text()
        if strings.HasPrefix(line, "Private_Clean:") || strings.HasPrefix(line, "Private_Dirty:") {
            if n, ok := parseKBLine(line); ok {
                kb += n
            }
        }
    }
    if err := sc.Err(); err != nil {
        return 0, err
    }
    return kb * 1024, nil
}

// privateFromSmaps 获取 Linux 上的内存统计（Private_Clean + Private_Dirty）
func privateFromSmaps(pid int) (uint64, error) {
    path := filepath.Join("/proc", strconv.Itoa(pid), "smaps")
    f, err := os.Open(path)
    if err != nil {
        return 0, err
    }
    defer f.Close()

    var kb uint64
    sc := bufio.NewScanner(f)
    for sc.Scan() {
        line := sc.Text()
        if strings.HasPrefix(line, "Private_Clean:") || strings.HasPrefix(line, "Private_Dirty:") {
            if n, ok := parseKBLine(line); ok {
                kb += n
            }
        }
    }
    if err := sc.Err(); err != nil {
        return 0, err
    }
    return kb * 1024, nil
}

// parseKBLine 解析内存行，提取单位为 kB 的数字
func parseKBLine(line string) (uint64, bool) {
    fs := strings.Fields(line) // e.g. ["Private_Clean:", "123", "kB"]
    if len(fs) < 2 {
        return 0, false
    }
    n, err := strconv.ParseUint(fs[1], 10, 64)
    if err != nil {
        return 0, false
    }
    return n, true
}

// isLinux 判断当前是否为 Linux 平台
func isLinux() bool {
    return strings.Contains(os.Getenv("GOOS"), "linux")
}

// isDarwin 判断当前是否为 macOS 平台
func isDarwin() bool {
    return strings.Contains(os.Getenv("GOOS"), "darwin")
}

// getMemoryBytesMac 获取 macOS 上的内存使用情况
func getMemoryBytesMac(pid int) (uint64, error) {
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
