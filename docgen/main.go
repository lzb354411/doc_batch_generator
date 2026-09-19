// docgen —— 文档批量生成工具
//
// 默认启动 Fyne 图形界面；指定 -config 时走命令行模式（阶段 1 验证入口），
// 读取 JSON 配置执行一次批量生成并打印日志与汇总：
//
//	docgen            启动 GUI
//	docgen -config run.json    CLI 模式（用于与 Python 版对比验证）
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"syscall"

	"docgen/internal/core"
	"docgen/internal/ui"
)

func main() {
	configPath := flag.String("config", "", "JSON 配置文件路径（留空则启动图形界面）")
	flag.Parse()

	if *configPath == "" {
		enableDPIAwareness()
		if err := ui.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "启动界面失败：", err)
			os.Exit(1)
		}
		return
	}

	runCLI(*configPath)
}

// enableDPIAwareness 声明进程为 DPI 感知，必须在创建任何窗口之前调用。
//
// 背景：Windows 系统显示缩放（本机 125%）下，DPI 不感知的进程会被系统
// 把窗口位图整体放大 1.25 倍显示——文字模糊、界面偏大。声明感知后，
// Fyne 会按系统实际 DPI（125%）原生渲染，清晰且尺寸正确。
//
// 按系统能力从新到旧依次尝试，全部失败则维持系统默认（不影响功能）。
func enableDPIAwareness() {
	user32 := syscall.NewLazyDLL("user32.dll")

	// Windows 10 1703+：按显示器感知 v2（多显示器各自 DPI 也正确）
	if proc := user32.NewProc("SetProcessDpiAwarenessContext"); proc.Find() == nil {
		// 参数 -4 = DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
		if ret, _, _ := proc.Call(^uintptr(3)); ret != 0 {
			return
		}
	}
	// Windows 8.1+：shcore 方式
	if proc := syscall.NewLazyDLL("shcore.dll").NewProc("SetProcessDpiAwareness"); proc.Find() == nil {
		if ret, _, _ := proc.Call(2); ret == 0 { // 2 = PROCESS_PER_MONITOR_DPI_AWARE，返回 0 = S_OK
			return
		}
	}
	// 更老的系统：仅系统级感知
	user32.NewProc("SetProcessDPIAware").Call()
}

// runCLI 命令行模式：读取 JSON 配置执行一次批量生成。
func runCLI(configPath string) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Println("读取配置失败：", err)
		os.Exit(1)
	}
	var cfg core.RunConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Println("解析配置失败：", err)
		os.Exit(1)
	}

	cb := &core.RunCallbacks{
		Log: func(line string) { fmt.Println(line) },
	}
	res := core.Run(cfg, cb)

	fmt.Println()
	for _, d := range res.Details {
		status := "成功"
		if d.Status != "success" {
			status = "失败"
		}
		fmt.Printf("[%s] %s — %s\n", status, d.Name, d.Note)
	}
	fmt.Println()
	fmt.Println(res.Summary)
	if !res.Success {
		os.Exit(1)
	}
}
