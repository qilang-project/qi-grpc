package interop

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	pb "qigrpc/interop/greetpb"
)

// 元数据是 gRPC 鉴权的载体。没有它，拦截器只能打日志，做不了鉴权，
// 客户端也没法调任何需要 token 的服务 —— 也就是绝大多数真服务。
//
// 服务端要设 REQUIRE_TOKEN=<令牌> 才启用鉴权拦截器。
func TestMetadataAuth(t *testing.T) {
	want := os.Getenv("REQUIRE_TOKEN")
	if want == "" {
		t.Skip("没设 REQUIRE_TOKEN，跳过")
	}
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

	// 不带令牌要被拦下
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = client.SayHello(ctx, &pb.HelloRequest{Name: "无票"})
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Fatalf("不带令牌要 Unauthenticated，拿到 %v", err)
	}

	// 带对的令牌要放行，并且能拿到服务端塞的响应元数据
	ctx2 := metadata.AppendToOutgoingContext(context.Background(),
		"authorization", "Bearer "+want)
	ctx2, cancel2 := context.WithTimeout(ctx2, 5*time.Second)
	defer cancel2()
	var trailer metadata.MD
	reply, err := client.SayHello(ctx2, &pb.HelloRequest{Name: "有票"}, grpc.Trailer(&trailer))
	if err != nil {
		t.Fatalf("带了令牌还被拦: %v", err)
	}
	if reply.Message != "你好，有票！" {
		t.Errorf("回错了: %q", reply.Message)
	}
	if got := trailer.Get("x-served-by"); len(got) == 0 || got[0] != "qi-grpc" {
		t.Errorf("服务端塞的响应元数据没收到: %v", trailer)
	}

	// 令牌不对也要被拦
	ctx3 := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer wrong-token")
	ctx3, cancel3 := context.WithTimeout(ctx3, 5*time.Second)
	defer cancel3()
	_, err = client.SayHello(ctx3, &pb.HelloRequest{Name: "假票"})
	st, _ = status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Fatalf("假令牌要 Unauthenticated，拿到 %v", err)
	}
}
