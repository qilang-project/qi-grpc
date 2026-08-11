// 用真的 Go gRPC 客户端打 qi 写的服务端。
//
// grpcurl 走的是「读 .proto 动态构造」那条路；Go 这边走的是 protoc 生成的
// 静态桩，两条路的编解码实现不一样，都通过才算真互通。
//
// 另外这里能验到 grpcurl 验不到的一件事：**一条连接上多路复用**。
// grpcurl 每次调用起一个进程、开一条新连接；Go 客户端复用同一条 HTTP/2
// 连接跑并发流，那才是 gRPC 的常态用法。
//
// 跑法（先起 qi 服务端）：
//   GREET_PROTO=$PWD/协议/greet.proto PORT=47813 ./问候服务端 &
//   cd 互通/go对照 && QI_GRPC_ADDR=127.0.0.1:47813 go test -v
package interop

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	pb "qigrpc/interop/greetpb"
)

func addr() string {
	if a := os.Getenv("QI_GRPC_ADDR"); a != "" {
		return a
	}
	return "127.0.0.1:47813"
}

func dial(t *testing.T) (pb.GreeterClient, func()) {
	t.Helper()
	conn, err := grpc.NewClient(addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连不上 %s: %v", addr(), err)
	}
	return pb.NewGreeterClient(conn), func() { conn.Close() }
}

func TestSayHello(t *testing.T) {
	client, done := dial(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reply, err := client.SayHello(ctx, &pb.HelloRequest{
		Name:  "Go",
		Times: 2,
		Mood:  pb.Mood_MOOD_HAPPY,
		Tags:  []string{"x", "y"},
	})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if want := "你好，Go！你好，Go！"; reply.Message != want {
		t.Errorf("message = %q，要的是 %q", reply.Message, want)
	}
	// 嵌套消息要能带回来
	if reply.Detail == nil || reply.Detail.Server != "qi-grpc" {
		t.Errorf("detail 不对: %+v", reply.Detail)
	}
	// int64 在线格式里是 varint，Go 这边直接是 int64（不是字符串）——
	// 正好验证「JSON 里是字符串」只是 JSON 映射的事，没漏进线格式
	if reply.ServedAt <= 0 {
		t.Errorf("servedAt 没填: %d", reply.ServedAt)
	}
}

func TestEnumReachesServer(t *testing.T) {
	client, done := dial(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reply, err := client.SayHello(ctx, &pb.HelloRequest{Name: "累了", Mood: pb.Mood_MOOD_TIRED})
	if err != nil {
		t.Fatalf("调用失败: %v", err)
	}
	if !strings.Contains(reply.Message, "早点休息") {
		t.Errorf("枚举没被服务端读到: %q", reply.Message)
	}
}

func TestErrorStatus(t *testing.T) {
	client, done := dial(t)
	defer done()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 空 name → InvalidArgument
	_, err := client.SayHello(ctx, &pb.HelloRequest{})
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Fatalf("要 InvalidArgument，拿到 %v", err)
	}
	if st.Message() != "name 不能为空" {
		t.Errorf("消息不对: %q", st.Message())
	}

	// 业务拒绝 → NotFound，且**中文消息要能原样穿过 trailers**
	// （trailers 是 HTTP 头，放不了非 ASCII，靠百分号编码带过去）
	_, err = client.SayHello(ctx, &pb.HelloRequest{Name: "禁用"})
	st, ok = status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Fatalf("要 NotFound，拿到 %v", err)
	}
	if !strings.Contains(st.Message(), "这个名字不给打招呼") {
		t.Errorf("中文消息没穿过 trailers: %q", st.Message())
	}
}

// 没注册的方法要**立刻**回 Unimplemented，不能挂着。
//
// 用 conn.Invoke 直接打一个不存在的方法名 —— 生成的桩里只有 .proto 里
// 那两个方法，构造不出「不存在的方法」。
func TestUnimplementedMethod(t *testing.T) {
	addr := os.Getenv("QI_GRPC_ADDR")
	if addr == "" {
		addr = "127.0.0.1:47813"
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("连不上: %v", err)
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = conn.Invoke(ctx, "/greet.Greeter/NoSuchMethod",
		&pb.HelloRequest{Name: "喂"}, &pb.HelloReply{})
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unimplemented {
		t.Fatalf("没注册的方法要回 Unimplemented，拿到 %v", err)
	}
}

// 一条连接上并发跑 50 个调用 —— 这是 grpcurl 验不到的多路复用
func TestMultiplexOnOneConn(t *testing.T) {
	client, done := dial(t)
	defer done()

	const concurrency = 50
	var wg sync.WaitGroup
	failures := make(chan error, concurrency)
	start := time.Now()
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			reply, err := client.SayHello(ctx, &pb.HelloRequest{Name: "并发", Times: 1})
			if err != nil {
				failures <- err
				return
			}
			if reply.Message != "你好，并发！" {
				failures <- status.Errorf(codes.Internal, "第 %d 个回错了: %q", n, reply.Message)
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		t.Fatalf("并发调用出错: %v", e)
	}
	t.Logf("一条连接跑完 %d 个并发调用，用时 %v", concurrency, time.Since(start))
}
