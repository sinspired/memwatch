# memwatch

一个用于监控指定进程内存变化的轻量命令行工具：启动时记录基线；当内存降至一半时触发“Half”事件；之后每下降固定步长（MB）触发“Step”事件；进程结束或出错时给出“Exit”。输出附带时间戳、PID、当前内存等信息。

---

## 特性

- **进程名解析优先级:** -name > -n > 第一个位置参数 > 默认值 subs-check。便于脚本与交互统一约定。
- **半程阈值提示:** 当监测到内存较峰值降至一半时输出 Half 事件，直观定位“开始回落”的时刻。
- **阶梯式回落跟踪:** 在 Half 之后，每下降 stepMB 触发一次 Step 事件，帮助量化回收速度与幅度。
- **友好输出与时间戳:** RFC3339 时间、PID、MB 为单位的内存读数，便于日志检索与比对。
- **优雅中断:** 支持 Ctrl+C；收到中断信号会安全停止监控并退出。

---

## 安装

- **使用 go install（推荐）:**
  ```bash
  go install github.com/sinspired/memwatch/cmd/memwatch@latest
  ```
- **从源码构建:**
  ```bash
  git clone https://github.com/sinspired/memwatch
  cd memwatch/cmd/memwatch
  go build -o memwatch
  ```
- **运行环境:**
  - **Go 版本:** 建议 Go 1.20+。
  - **平台:** 主流桌面/服务器平台（Windows/macOS/Linux），以目标平台的进程探测权限为准。

---

## 使用

```text
Usage: memwatch [options] [process_name]

Options:
  -name string
        process name (Windows 会自动匹配 .exe) (default "subs-check")
  -n string
        process name (与 -name 等价的短别名) (default "subs-check")
  -interval duration
        sampling interval (default 1s)
  -stepMB int
        step size after half, in MB (default 10)

优先级: -name > -n > 第一个位置参数 > 默认值 subs-check
示例:
  memwatch -name myproc
  memwatch -n myproc
  memwatch myproc
  memwatch -interval 500ms -stepMB 20 myproc
```

- **进程名解析规则:**
  - **-name:** 最高优先级。
  - **-n:** 当 -name 未显式指定时生效。
  - **位置参数:** 当 -name 和 -n 都为默认值时，取第一个位置参数。
  - **默认值:** 都未指定时使用 subs-check。
- **参数说明:**
  - **-interval:** 采样间隔（如 200ms、1s、2s）；越短越实时，开销越高。
  - **-stepMB:** Half 之后每下降多少 MB 触发 Step；必须 > 0（否则以参数错误退出）。
  - **Windows 名称匹配:** 指定 myproc 时会自动匹配 myproc.exe。

---

## 示例

- **最简用法（位置参数作为进程名）:**
  ```bash
  memwatch myproc
  ```
- **更细采样与更大步长:**
  ```bash
  memwatch -interval 500ms -stepMB 20 myproc
  ```
- **显式指定名称（长/短旗标）:**
  ```bash
  memwatch -name myproc
  memwatch -n myproc
  ```

- **示例输出:**
  ```text
  [2025-08-12T22:51:04+08:00] Start: PID=12345 mem=120.34 MB
  [2025-08-12T22:52:10+08:00] Half reached in 1m6s, mem=60.12 MB
  [2025-08-12T22:52:15+08:00] Down 1 step(s) of 10MB, mem=49.95 MB
  [2025-08-12T22:52:20+08:00] Down 2 step(s) of 10MB, mem=39.80 MB
  [2025-08-12T22:53:02+08:00] Exit
  ```

- **事件含义:**
  - **Start:** 找到目标进程，开始监控。
  - **Half:** 内存自峰值回落至一半所用时间与此刻内存。
  - **Step:** 在 Half 之后按 stepMB 阶梯下降的次数与当前内存。
  - **Exit:** 进程退出或监控结束；如伴随错误，将打印错误并以非零码退出。

---

## 退出码与信号

- **0:** 正常结束（包括手动 Ctrl+C）。
- **1:** 运行期错误（如监控过程中出现非中断性的错误）。
- **2:** 参数错误（例如 stepMB <= 0）。
- **中断信号:** Ctrl+C 触发优雅退出；会停止采样并结束事件循环。

---

## 备注

- **权限需求:** 某些平台/场景下监控其他用户的进程需要提升权限；无权限时可能无法获取内存数据。
- **采样影响:** 过短的 -interval 会增加系统调用频率；根据场景在实时性与开销间权衡。
- **时区与时间格式:** 输出采用 RFC3339；便于日志聚合与跨时区比对。