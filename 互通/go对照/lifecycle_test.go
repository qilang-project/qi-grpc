package interop

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pb "qigrpc/interop/greetpb"
)

// 客户端中途取消，服务端要**发现**并停手，而不是把活干完再往一个死掉的流上写。
//
// 「收一条返回 -1」表达不了这件事 —— 那个的含义是「客户端发完了」，
// 一元调用的正常路径每次都会走到那儿。
func TestServerSeesClientGone(t *testing.T) {
	addr := os.Getenv("QI_GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:47813"
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	defer conn.Close()

	// 服务端在这条路径上边磨蹭边看人还在不在（最多 5 秒）
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = pb.NewGreeterClient(conn).SayHello(ctx, &pb.HelloRequest{Name: "看着走"})
		close(done)
	}()
	// 半秒后撤
	time.Sleep(500 * time.Millisecond)
	cancel()
	<-done

	// 服务端应该在一秒内就发现并收工；给它一点时间打日志
	time.Sleep(700 * time.Millisecond)

	// 之后服务端要照常干活（没被那次取消搞坏）
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	reply, err := pb.NewGreeterClient(conn).SayHello(ctx2, &pb.HelloRequest{Name: "还好吗"})
	if err != nil {
		t.Fatalf("取消之后服务端不干活了: %v", err)
	}
	if reply.Message != "你好，还好吗！" {
		t.Errorf("回错了: %q", reply.Message)
	}
}
