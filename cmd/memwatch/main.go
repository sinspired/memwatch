package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/sinspired/memwatch/memwatch"
)

func main() {
	var (
		interval time.Duration
		stepMB   int

		// 为了判断用户是否显式传入，使用单独变量并对比默认值
		defaultName = "subs-check"
		nameLong    string
		nameShort   string
	)

	flag.StringVar(&nameLong, "name", defaultName, "process name (Windows 会自动匹配 .exe)")
	flag.StringVar(&nameShort, "n", defaultName, "process name (与 -name 等价的短别名)")
	flag.DurationVar(&interval, "interval", time.Second, "sampling interval")
	flag.IntVar(&stepMB, "stepMB", 10, "step size after big change, in MB")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [process_name]\n\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Options:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, `
优先级: -name > -n > 第一个位置参数 > 默认值 subs-check
示例:
  memwatch -name myproc
  memwatch -n myproc
  memwatch myproc
  memwatch -interval 500ms -stepMB 20 myproc`)
	}

	flag.Parse()

	// 解析进程名优先级：-name > -n > 位置参数 > 默认值
	name := nameLong
	if nameLong == defaultName && nameShort != defaultName {
		name = nameShort
	}
	if name == defaultName && flag.NArg() > 0 {
		name = flag.Arg(0)
	}

	if stepMB <= 0 {
		fmt.Println("stepMB must be > 0")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	events, err := memwatch.Watch(ctx, name, interval, uint64(stepMB)*1024*1024)
	if err != nil {
		fmt.Println("启动失败：", err)
		os.Exit(1)
	}

	for ev := range events {
		switch ev.Kind {
		case memwatch.EventStart:
			fmt.Printf("[%s] Start(First print at >=20%% changed): PID=%d mem=%.2f MB\n",
				ev.Time.Format(time.RFC3339),
				ev.PID,
				float64(ev.MemoryBytes)/1024/1024)
		case memwatch.EventBigChange:
			fmt.Printf("[%s] BigChange(>=20%%) reached in %s, mem=%.2f MB\n",
				ev.Time.Format(time.RFC3339),
				ev.Elapsed,
				float64(ev.MemoryBytes)/1024/1024)
		case memwatch.EventStep:
			fmt.Printf("[%s] Down %d step(s) of %dMB, mem=%.2f MB\n",
				ev.Time.Format(time.RFC3339),
				ev.StepCount, stepMB,
				float64(ev.MemoryBytes)/1024/1024)
		case memwatch.EventExit:
			if ev.Err != nil && ctx.Err() == nil {
				fmt.Printf("[%s] Exit: %v\n", ev.Time.Format(time.RFC3339), ev.Err)
				os.Exit(1)
			} else {
				fmt.Printf("[%s] Exit\n", ev.Time.Format(time.RFC3339))
			}
		}
	}
}
