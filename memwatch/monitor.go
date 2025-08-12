package memwatch

import (
    "context"
    "errors"
    "time"
)

type EventKind int

const (
    EventStart EventKind = iota // 启动时记录
    EventHalf                   // 降到初始的一半
    EventStep                   // 半数后每下降一个步进（默认 10MB）
    EventExit                   // 进程退出或监测结束
)

type Event struct {
    Kind         EventKind
    Time         time.Time
    PID          int
    MemoryBytes  uint64
    Elapsed      time.Duration // 对 Half 有意义
    StepCount    int           // 本次跨过多少个 10MB 阶梯（>=1）。如果你需要“每 10MB 一条”，可展开打印。
    Err          error         // Exit 时可能带错误
}

// Watch 监测指定进程名，周期采样，返回事件通道
// - procName: 如 "subs-check"；Windows 下会自动匹配 "subs-check.exe"
// - interval: 采样周期
// - stepBytes: 半数后每下降的步进（建议 10*1024*1024）
// 注意：返回的通道由内部协程关闭。若启动阶段找不到进程，返回 error。
func Watch(ctx context.Context, procName string, interval time.Duration, stepBytes uint64) (<-chan Event, error) {
    if stepBytes == 0 {
        return nil, errors.New("stepBytes must be > 0")
    }
    pid, err := findPIDByName(procName)
    if err != nil {
        return nil, err
    }

    // 取初始内存
    mem0, err := getMemoryBytes(pid)
    if err != nil {
        return nil, err
    }

    events := make(chan Event, 8)
    go func() {
        defer close(events)

        start := time.Now()
        events <- Event{Kind: EventStart, Time: start, PID: pid, MemoryBytes: mem0}

        half := mem0 / 2
        halfDone := false

        // 半数后，基于“桶”的方式做等距阶梯：每跨过一个 stepBytes 触发一次
        // 桶号 = mem / stepBytes
        var prevBucket uint64

        ticker := time.NewTicker(interval)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                events <- Event{Kind: EventExit, Time: time.Now(), PID: pid, Err: ctx.Err()}
                return
            case <-ticker.C:
                mem, err := getMemoryBytes(pid)
                if err != nil {
                    events <- Event{Kind: EventExit, Time: time.Now(), PID: pid, Err: err}
                    return
                }

                now := time.Now()
                if !halfDone && mem <= half {
                    halfDone = true
                    elapsed := now.Sub(start)
                    events <- Event{
                        Kind:        EventHalf,
                        Time:        now,
                        PID:         pid,
                        MemoryBytes: mem,
                        Elapsed:     elapsed,
                    }
                    prevBucket = mem / stepBytes
                    continue
                }

                if halfDone {
                    bucket := mem / stepBytes
                    if bucket < prevBucket {
                        // 可能一次下降跨过多个 10MB 阶梯
                        steps := int(prevBucket - bucket)
                        events <- Event{
                            Kind:        EventStep,
                            Time:        now,
                            PID:         pid,
                            MemoryBytes: mem,
                            StepCount:   steps,
                        }
                        prevBucket = bucket
                    }
                }
            }
        }
    }()
    return events, nil
}
