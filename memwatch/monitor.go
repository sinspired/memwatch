package memwatch

import (
	"context"
	"errors"
	"time"
)

type EventKind int

const (
	EventStart     EventKind = iota // 启动时记录
	EventBigChange                  // 第一次变动达到±20% 时记录（相对初始值，增减都触发）
	EventStep                       // 首次触发后，每下降一个步进（默认 10MB）
	EventExit                       // 进程退出或监测结束
)

type Event struct {
	Kind        EventKind
	Time        time.Time
	PID         int
	MemoryBytes uint64
	Elapsed     time.Duration // 对 EventHalf 有意义
	StepCount   int           // 本次跨过多少个 10MB 阶梯（>=1）。如果你需要“每 10MB 一条”，可展开打印。
	Err         error         // Exit 时可能带错误
}

// Watch 监测指定进程名，周期采样，返回事件通道
// - procName: 如 "subs-check"；Windows 下会自动匹配 "subs-check.exe"
// - interval: 采样周期
// - stepBytes: 首次触发后，每次“下降”触发的步进（建议 10*1024*1024）
// 规则：
// - EventStart：启动时输出一次
// - EventHalf：当内存相对初始值的绝对变化量 ≥ 20%（增减皆可）时输出一次
// - EventStep：在 EventHalf 之后，仅当内存继续下降并跨过 stepBytes 等距阶梯时输出
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

		// 首次触发阈值：20%（绝对变化量）
		threshold := mem0 / 5 // 20%
		if threshold == 0 {
			threshold = 1 // 防止极小初值导致立刻触发
		}

		firstDone := false

		// 第一次触发后，基于“桶”的方式做等距阶梯：每跨过一个 stepBytes 触发一次
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

				// 第一次变动达到±20%（相对初始值）时触发
				if !firstDone && absDiff(mem, mem0) >= threshold {
					firstDone = true
					elapsed := now.Sub(start)
					events <- Event{
						Kind:        EventBigChange,
						Time:        now,
						PID:         pid,
						MemoryBytes: mem,
						Elapsed:     elapsed,
					}
					prevBucket = mem / stepBytes
					continue
				}

				// 首次触发后，仅在“下降”方向按步进触发
				if firstDone {
					bucket := mem / stepBytes
					if bucket < prevBucket {
						// 可能一次下降跨过多个步进
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

// absDiff 计算两个 uint64 的绝对差值
func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
