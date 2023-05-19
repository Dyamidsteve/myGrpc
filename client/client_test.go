package client

import (
	"context"
	"fmt"
	mygrpc "myGprc"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

type Bar int

func (b Bar) TimeOut(argv int, reply *int) error {
	time.Sleep(time.Second * 2)
	return nil
}

type NilArgs struct{}

func startServer(addr chan string) {
	var b Bar
	_ = mygrpc.Register(&b)
	l, _ := net.Listen("tcp", ":0")
	addr <- l.Addr().String()
	mygrpc.Accept(l)
}

func _assert(condition bool, msg string, v ...any) {
	if !condition {
		panic(fmt.Sprintf("assertion failed:"+msg, v...))
	}

}

func TestClient_dialTimeOut(t *testing.T) {
	t.Parallel()
	lis, _ := net.Listen("tcp", ":0")

	f := func(conn net.Conn, opt *mygrpc.Option) (client *Client, err error) {
		_ = conn.Close()
		time.Sleep(time.Second * 2)
		return nil, nil
	}

	//测试设置超时
	t.Run("timeout", func(t *testing.T) {
		//相比开放的Dial方法，dialTime
		_, err := dialTimeout(f, "tcp", lis.Addr().String(), &mygrpc.Option{ConnectTimeout: time.Second})
		_assert(err != nil && strings.Contains(err.Error(), "connect timeout"), "expected a timeout error")

	})

	//测试不设置超时
	t.Run("0", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", lis.Addr().String(), &mygrpc.Option{ConnectTimeout: 0})
		_assert(err == nil, "0 means no limit")

	})
}

func TestClient_Call(t *testing.T) {
	t.Parallel()
	addrCh := make(chan string)
	go startServer(addrCh)
	addr := <-addrCh
	time.Sleep(time.Second)
	//客户端等待服务端处理超时
	t.Run("client timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		err := client.Call(ctx, "Bar.TimeOut", 1, &reply)
		_assert(err != nil && strings.Contains(err.Error(), ctx.Err().Error()), "expected a timeout error")
	})

	t.Run("server handle timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		err := client.Call(ctx, "Bar.TimeOut", 1, &reply)
		//"handle timeout"
		_assert(err != nil && strings.Contains(err.Error(), ctx.Err().Error()), "expected a timeout error")

	})
}

func TestXDial(t *testing.T) {
	if runtime.GOOS == "linux" {
		ch := make(chan struct{})
		addr := "/tmp/geerpc.sock"
		go func() {
			_ = os.Remove(addr)
			lis, err := net.Listen("unix", addr)
			if err != nil {
				t.Fatal("failed to listen unix socket")
			}
			ch <- struct{}{}
			mygrpc.Accept(lis)

		}()
		<-ch
		_, err := XDial("unix@" + addr)
		_assert(err == nil, "failed to connect unix socket")

	} else {
		return
	}
}
