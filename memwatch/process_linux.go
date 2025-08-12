//go:build linux

package memwatch

import (
    "bufio"
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"
)

func findPIDByName(name string) (int, error) {
    // 在 Linux 下，优先匹配 /proc/<pid>/comm 的完整程序名
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
    return 0, fmt.Errorf("process %q not found", name)
}

func getMemoryBytes(pid int) (uint64, error) {
    if v, err := privateFromSmapsRollup(pid); err == nil {
        return v, nil
    } else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
        return 0, err
    }
    return privateFromSmaps(pid)
}

// Private_Clean + Private_Dirty (smaps_rollup)
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

// Private_Clean + Private_Dirty (smaps 累加)
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
