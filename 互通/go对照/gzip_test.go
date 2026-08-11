package interop

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	pb "qigrpc/interop/greetpb"
)

// Go 客户端开 gzip 打 qi 服务端。
//
// 验两个方向：Go 压着发（qi 服务端要解得开），qi 压着回（Go 要解得开）。
// 请求要够大才会真压 —— 小消息压了反而变大，两边都设了门槛。
func TestGzipBothWays(t *testing.T) {
	addr := os.Getenv("QI_GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:47813"
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 名字很长 → 请求超过门槛会被压；响应重复 5 遍更长 → 回程也会压
	longName := strings.Repeat("超长的名字", 200)
	reply, err := pb.NewGreeterClient(conn).SayHello(ctx,
		&pb.HelloRequest{Name: longName, Times: 5},
		grpc.UseCompressor(gzip.Name))
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	want := strings.Repeat("你好，"+longName+"！", 5)
	if reply.Message != want {
		t.Fatalf("回的内容不对，长度 %d，要的是 %d", len(reply.Message), len(want))
	}
	t.Logf("请求 %d 字节、响应 %d 字节，双向 gzip 通过", len(longName), len(reply.Message))
}
