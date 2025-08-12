//go:build windows

package memwatch

import (
    "fmt"
    "strings"
    "syscall"
    "time"
    "unicode/utf16"
    "unsafe"
)

// ---------- find PID by process name (Toolhelp) ----------

var (
    kernel32                     = syscall.NewLazyDLL("kernel32.dll")
    procCreateToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
    procProcess32FirstW          = kernel32.NewProc("Process32FirstW")
    procProcess32NextW           = kernel32.NewProc("Process32NextW")
)

const (
    TH32CS_SNAPPROCESS = 0x00000002
    MAX_PATH           = 260
)

type processEntry32 struct {
    DwSize              uint32
    CntUsage            uint32
    Th32ProcessID       uint32
    Th32DefaultHeapID   uintptr
    Th32ModuleID        uint32
    CntThreads          uint32
    Th32ParentProcessID uint32
    PcPriClassBase      int32
    DwFlags             uint32
    SzExeFile           [MAX_PATH]uint16
}

func findPIDByName(name string) (int, error) {
    want := strings.ToLower(name)
    if !strings.HasSuffix(want, ".exe") {
        // 宽松匹配：允许传入 "subs-check" 匹配 "subs-check.exe"
        wantExe := want + ".exe"
        pid, err := findPIDByLowerNameAny([]string{want, wantExe})
        if err == nil {
            return pid, nil
        }
        return 0, err
    }
    return findPIDByLowerNameAny([]string{want})
}

func findPIDByLowerNameAny(cands []string) (int, error) {
    snap, _, _ := procCreateToolhelp32Snapshot.Call(TH32CS_SNAPPROCESS, 0)
    if snap == uintptr(syscall.InvalidHandle) {
        return 0, fmt.Errorf("CreateToolhelp32Snapshot failed")
    }
    defer syscall.CloseHandle(syscall.Handle(snap))

    var pe processEntry32
    pe.DwSize = uint32(unsafe.Sizeof(pe))

    r1, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&pe)))
    if r1 == 0 {
        return 0, fmt.Errorf("Process32FirstW failed")
    }

    for {
        name := toLowerString(pe.SzExeFile[:])
        for _, c := range cands {
            if name == c {
                return int(pe.Th32ProcessID), nil
            }
        }
        r1, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&pe)))
        if r1 == 0 {
            break
        }
    }
    return 0, fmt.Errorf("process %q not found", cands[0])
}

func toLowerString(wsz []uint16) string {
    n := 0
    for n < len(wsz) && wsz[n] != 0 {
        n++
    }
    return strings.ToLower(string(utf16.Decode(wsz[:n])))
}

// ---------- memory: Private Working Set (via PDH) ----------

var (
    modPdh                    = syscall.NewLazyDLL("pdh.dll")
    procPdhOpenQueryW         = modPdh.NewProc("PdhOpenQueryW")
    procPdhAddEnglishCounterW = modPdh.NewProc("PdhAddEnglishCounterW")
    procPdhCollectQueryData   = modPdh.NewProc("PdhCollectQueryData")
    procPdhGetFormattedArrayW = modPdh.NewProc("PdhGetFormattedCounterArrayW")
    procPdhCloseQuery         = modPdh.NewProc("PdhCloseQuery")
)

const (
    PDH_FMT_LARGE = 0x00000400
    ERROR_SUCCESS = 0
    PDH_MORE_DATA = 0x800007D2
)

type (
    HQUERY   uintptr
    HCOUNTER uintptr
)

type pdhFmtLarge struct {
    CStatus    uint32
    _          uint32
    LargeValue int64
}

type pdhItemRaw struct {
    SzName   *uint16
    FmtValue pdhFmtLarge
}

// 获取给定 PID 的 “Working Set - Private” 字节数
func getMemoryBytes(pid int) (uint64, error) {
    // 打开查询
    var hq HQUERY
    if r1, _, e := procPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&hq))); r1 != ERROR_SUCCESS {
        return 0, fmt.Errorf("PdhOpenQuery: 0x%x (%v)", r1, e)
    }
    defer procPdhCloseQuery.Call(uintptr(hq))

    // 加入两个计数器（实例通配）：ID Process, WS Private
    var idCounter, wsCounter HCOUNTER
    if r1, _, e := procPdhAddEnglishCounterW.Call(
        uintptr(hq),
        uintptr(unsafe.Pointer(utf16Ptr(`\Process(*)\ID Process`))),
        0,
        uintptr(unsafe.Pointer(&idCounter)),
    ); r1 != ERROR_SUCCESS {
        return 0, fmt.Errorf("PdhAddEnglishCounter(ID): 0x%x (%v)", r1, e)
    }
    if r1, _, e := procPdhAddEnglishCounterW.Call(
        uintptr(hq),
        uintptr(unsafe.Pointer(utf16Ptr(`\Process(*)\Working Set - Private`))),
        0,
        uintptr(unsafe.Pointer(&wsCounter)),
    ); r1 != ERROR_SUCCESS {
        return 0, fmt.Errorf("PdhAddEnglishCounter(WS-Private): 0x%x (%v)", r1, e)
    }

    // 采样
    if r1, _, e := procPdhCollectQueryData.Call(uintptr(hq)); r1 != ERROR_SUCCESS {
        return 0, fmt.Errorf("PdhCollectQueryData: 0x%x (%v)", r1, e)
    }
    // PDH 对应到活动专用工作集是瞬时计数器，一次采样即可
    // 给系统一点点缓冲（在非常忙的系统上，可选）
    time.Sleep(10 * time.Millisecond)
    if r1, _, e := procPdhCollectQueryData.Call(uintptr(hq)); r1 != ERROR_SUCCESS {
        return 0, fmt.Errorf("PdhCollectQueryData(2): 0x%x (%v)", r1, e)
    }

    // 取数组，映射 PID -> 实例名
    idItems, err := pdhGetFormattedArray(idCounter)
    if err != nil {
        return 0, err
    }
    wsItems, err := pdhGetFormattedArray(wsCounter)
    if err != nil {
        return 0, err
    }

    var instance string
    for _, it := range idItems {
        if it.FmtValue.CStatus == ERROR_SUCCESS && uint32(it.FmtValue.LargeValue) == uint32(pid) {
            instance = utf16ToString(it.SzName)
            break
        }
    }
    if instance == "" {
        return 0, fmt.Errorf("PID %d not found", pid)
    }

    for _, it := range wsItems {
        if utf16ToString(it.SzName) == instance {
            if it.FmtValue.CStatus != ERROR_SUCCESS {
                return 0, fmt.Errorf("counter invalid: 0x%x", it.FmtValue.CStatus)
            }
            return uint64(it.FmtValue.LargeValue), nil
        }
    }
    return 0, fmt.Errorf("no WS Private value for %s", instance)
}

func pdhGetFormattedArray(counter HCOUNTER) ([]pdhItemRaw, error) {
    var bufSize, count uint32
    r1, _, _ := procPdhGetFormattedArrayW.Call(
        uintptr(counter),
        uintptr(PDH_FMT_LARGE),
        uintptr(unsafe.Pointer(&bufSize)),
        uintptr(unsafe.Pointer(&count)),
        0,
    )
    if r1 != PDH_MORE_DATA {
        return nil, fmt.Errorf("PdhGetFormattedArray probe: 0x%x", r1)
    }

    buf := make([]byte, bufSize)
    r1, _, e := procPdhGetFormattedArrayW.Call(
        uintptr(counter),
        uintptr(PDH_FMT_LARGE),
        uintptr(unsafe.Pointer(&bufSize)),
        uintptr(unsafe.Pointer(&count)),
        uintptr(unsafe.Pointer(&buf[0])),
    )
    if r1 != ERROR_SUCCESS {
        return nil, fmt.Errorf("PdhGetFormattedArray data: 0x%x (%v)", r1, e)
    }

    itemSize := unsafe.Sizeof(pdhItemRaw{})
    base := uintptr(unsafe.Pointer(&buf[0]))
    out := make([]pdhItemRaw, 0, count)
    for i := uint32(0); i < count; i++ {
        ptr := (*pdhItemRaw)(unsafe.Pointer(base + uintptr(i)*itemSize))
        out = append(out, *ptr)
    }
    return out, nil
}

func utf16Ptr(s string) *uint16 {
    u, _ := syscall.UTF16PtrFromString(s)
    return u
}

func utf16ToString(p *uint16) string {
    if p == nil {
        return ""
    }
    var buf []uint16
    base := uintptr(unsafe.Pointer(p))
    for i := 0; ; i++ {
        u := *(*uint16)(unsafe.Pointer(base + uintptr(i*2)))
        if u == 0 {
            break
        }
        buf = append(buf, u)
    }
    return string(utf16.Decode(buf))
}
