package interop

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

// 反射的四种请求都真打一遍。grpcurl 只用得到其中两种，
// 另外两种（扩展相关）要用反射客户端直接问。
func TestReflectionAllRequests(t *testing.T) {
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
	client := grpcreflect.NewClient(ctx, reflectpb.NewServerReflectionClient(conn))
	defer client.Reset()

	// 1. list_services
	services, err := client.ListServices()
	if err != nil {
		t.Fatalf("ListServices 失败: %v", err)
	}
	found := false
	for _, s := range services {
		if s == "greet.Greeter" {
			found = true
		}
	}
	if !found {
		t.Fatalf("服务列表里没有 greet.Greeter: %v", services)
	}

	// 2. file_containing_symbol
	fd, err := client.FileContainingSymbol("greet.Greeter")
	if err != nil {
		t.Fatalf("FileContainingSymbol 失败: %v", err)
	}
	if fd.GetName() != "greet.proto" && fd.GetName() != "协议/greet.proto" {
		t.Logf("文件名: %s", fd.GetName())
	}

	// 3. file_by_filename —— 用上一步拿到的真实文件名
	if _, err := client.FileByFilename(fd.GetName()); err != nil {
		t.Fatalf("FileByFilename(%s) 失败: %v", fd.GetName(), err)
	}

	// 4. all_extension_numbers_of_type —— 这条 grpcurl 用不到，
	//    但 Postman / grpcui 构造带扩展的消息时要靠它
	nums, err := client.AllExtensionNumbersForType("extdemo.Base")
	if err != nil {
		t.Fatalf("AllExtensionNumbersForType 失败: %v", err)
	}
	if len(nums) != 2 {
		t.Fatalf("要两个扩展号（100 和 137），拿到 %v", nums)
	}
	t.Logf("扩展号: %v", nums)

	// 5. file_containing_extension
	efd, err := client.FileContainingExtension("extdemo.Base", 137)
	if err != nil {
		t.Fatalf("FileContainingExtension 失败: %v", err)
	}
	t.Logf("扩展 137 定义在: %s", efd.GetName())
}
