package interop

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	pb "qigrpc/interop/greetpb"
)

// Go 客户端的 context deadline 会变成 grpc-timeout 头发过来。
// 服务端要**自己**掐掉并回 DeadlineExceeded，而不是等客户端那侧超时 ——
// 后者服务端日志上什么都看不到，前者能看出是谁给的期限。
func TestDeadlinePropagated(t *testing.T) {
	addr := os.Getenv("QI_GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:47813"
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	defer conn.Close()
	client := pb.NewGreeterClient(conn)

	// 服务端在这条路径上睡 3 秒，我们只给 800 毫秒
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = client.SayHello(ctx, &pb.HelloRequest{Name: "慢"})
	elapsed := time.Since(start)

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.DeadlineExceeded {
		t.Fatalf("要 DeadlineExceeded，拿到 %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("等了 %v 才回 —— 不像是服务端主动掐的", elapsed)
	}
	t.Logf("%v 就回了 DeadlineExceeded（服务端睡 3 秒）", elapsed)

	// 期限够长时同一条路径要正常返回
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	reply, err := client.SayHello(ctx2, &pb.HelloRequest{Name: "慢"})
	if err != nil {
		t.Fatalf("给够时间还是失败: %v", err)
	}
	if reply.Message != "你好，慢工出细活！" {
		t.Errorf("回错了: %q", reply.Message)
	}
}
