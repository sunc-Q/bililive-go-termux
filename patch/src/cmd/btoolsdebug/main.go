// btoolsdebug：本地手动验证 btoolssrv 抖音解析链路的调试入口。
// 用法:
//   btoolsdebug channel <直播间URL>   打印频道信息
//   btoolsdebug stream  <webRid>      打印流地址
//   btoolsdebug serve                 以服务模式监听 18110（配合 curl 调试）
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/bililive-go/bililive-go/src/pkg/btoolssrv"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: btoolsdebug channel|stream|serve [参数]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "channel":
		info, err := btoolssrv.ResolveChannelInfoFromURLPublic(os.Args[2])
		dump(info, err)
	case "stream":
		info, err := btoolssrv.GetStreamInfoPublic(os.Args[2])
		dump(info, err)
	case "serve":
		btoolssrv.Start()
		fmt.Println("serving on 127.0.0.1:18110, Ctrl+C 退出")
		select {}
	case "home":
		ids, err := btoolssrv.DebugHomeLiveRooms()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			os.Exit(1)
		}
		fmt.Println(ids)
	case "raw":
		b, err := btoolssrv.DebugRawEnter(os.Args[2])
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR:", err)
			os.Exit(1)
		}
		if len(b) > 2000 {
			b = b[:2000]
		}
		fmt.Println(string(b))
	default:
		fmt.Fprintln(os.Stderr, "未知子命令:", os.Args[1])
		os.Exit(2)
	}
}

func dump(v any, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
	time.Sleep(100 * time.Millisecond)
}
