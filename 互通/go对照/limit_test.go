package interop

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	pb "qigrpc/interop/greetpb"
)

// 超大消息要被**干净地拒绝**，而不是把服务端撑死。
//
// 没有上限的话，分帧头里那 4 字节是对面说了算的，照着它 extend 就是
// 一条声称 2GB 的消息把进程 OOM 掉 —— 而且不需要真发 2GB。
func TestOversizedRejected(t *testing.T) {
	addr := os.Getenv("QI_GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:47813"
	}
	// Go 默认的发送上限也是 4MB，得放开才能把大消息发出去打到服务端
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallSendMsgSize(64*1024*1024)))
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// 8MB 名字，超过默认 4MB 上限
	huge := strings.Repeat("x", 8*1024*1024)
	_, err = pb.NewGreeterClient(conn).SayHello(ctx, &pb.HelloRequest{Name: huge})
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("要一个 gRPC 状态，拿到 %v", err)
	}
	if st.Code() != codes.ResourceExhausted {
		t.Fatalf("要 ResourceExhausted，拿到 %v: %v", st.Code(), st.Message())
	}
	t.Logf("8MB 请求被干净拒绝: %v", st.Message())

	// 服务端还得活着
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	reply, err := pb.NewGreeterClient(conn).SayHello(ctx2, &pb.HelloRequest{Name: "还活着吗"})
	if err != nil {
		t.Fatalf("拒绝超大消息之后服务端不干活了: %v", err)
	}
	if reply.Message != "你好，还活着吗！" {
		t.Errorf("回错了: %q", reply.Message)
	}
}
