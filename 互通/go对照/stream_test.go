package interop

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pb "qigrpc/interop/greetpb"
)

func streamDial(t *testing.T) (pb.GreeterClient, func()) {
	t.Helper()
	addr := os.Getenv("QI_GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:47813"
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	return pb.NewGreeterClient(conn), func() { conn.Close() }
}

// 双向流：发 5 条、收 5 条，然后半关，服务端收尾。
//
// 这里能抓到的东西 grpcurl 抓不到：**发完不半关也要能收到回应**。
// 服务端要是等 END_STREAM 才处理，这个测试会挂到超时。
func TestBidiStream(t *testing.T) {
	client, done := streamDial(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := client.Chatter(ctx)
	if err != nil {
		t.Fatalf("开流失败: %v", err)
	}

	const n = 5
	for i := 1; i <= n; i++ {
		if err := stream.Send(&pb.HelloRequest{Name: fmt.Sprintf("第%d位", i)}); err != nil {
			t.Fatalf("第 %d 条发不出去: %v", i, err)
		}
		// 发一条立刻收一条 —— 真正的双向，不是「全发完再全收」
		reply, err := stream.Recv()
		if err != nil {
			t.Fatalf("第 %d 条收不到: %v", i, err)
		}
		want := fmt.Sprintf("第 %d 声：你好，第%d位！", i, i)
		if reply.Message != want {
			t.Fatalf("第 %d 条回错了: %q，要的是 %q", i, reply.Message, want)
		}
	}

	if err := stream.CloseSend(); err != nil {
		t.Fatalf("半关失败: %v", err)
	}
	// 服务端收尾之后应当是干净的 EOF
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("要 EOF，拿到 %v", err)
	}
}

// 客户端流的用法：连发几条不收，最后半关再收
func TestClientStreamStyle(t *testing.T) {
	client, done := streamDial(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := client.Chatter(ctx)
	if err != nil {
		t.Fatalf("开流失败: %v", err)
	}
	for i := 1; i <= 3; i++ {
		if err := stream.Send(&pb.HelloRequest{Name: strings.Repeat("啊", i)}); err != nil {
			t.Fatalf("发不出去: %v", err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("半关失败: %v", err)
	}
	got := 0
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("收出错: %v", err)
		}
		got++
	}
	if got != 3 {
		t.Fatalf("发了 3 条只收到 %d 条", got)
	}
}
