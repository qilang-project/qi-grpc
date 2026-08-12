#!/usr/bin/env bash
# qi-grpc 的全套验收：自己起服务端、跑完、关掉。
#
# 不接进 CI 的话，以后有人改运行时（h2、分帧、trailers、压缩…），
# 这些路径**不会有任何东西报警** —— 而 gRPC 出问题的方式往往是「挂着不动」
# 或者「静默丢消息」，不是干脆的崩溃，光看别的测试全绿完全发现不了。
#
# 用法：
#   ./跑验收.sh              # 全套
#   ./跑验收.sh --跳过go     # 没有 Go 时只跑 qi 侧和 grpcurl
#
# 环境变量：
#   QI_BIN          qi 编译器（默认按 PATH 找）
#   QI_RUNTIME_LIB  运行时归档
set -uo pipefail

cd "$(dirname "$0")"
ROOT=$PWD

QI=${QI_BIN:-qi}
PORT_QI=${GRPC_TEST_PORT:-47861}
PORT_GO=${GO_TEST_PORT:-47862}
SKIP_GO=0
[ "${1:-}" = "--跳过go" ] && SKIP_GO=1

TMP=$(mktemp -d)
QI_PID=""
GO_PID=""

cleanup() {
    [ -n "$QI_PID" ] && kill -9 "$QI_PID" 2>/dev/null
    [ -n "$GO_PID" ] && kill -9 "$GO_PID" 2>/dev/null
    rm -rf "$TMP"
}
trap cleanup EXIT

FAILED=0
ok() { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; FAILED=$((FAILED + 1)); }

echo "▶ 编译"
for one in 示例/问候服务端.qi 示例/问候客户端.qi 示例/流式客户端.qi 示例/压缩验.qi; do
    if $QI compile "$one" -o "$TMP/$(basename "${one%.qi}")" > "$TMP/编译.log" 2>&1; then
        ok "$one"
    else
        bad "$one"
        tail -5 "$TMP/编译.log"
    fi
done
[ $FAILED -gt 0 ] && exit 1

echo "▶ 起 qi 服务端 :$PORT_QI"
GREET_PROTO="$ROOT/协议/greet.proto,$ROOT/协议/ext.proto" PORT=$PORT_QI \
    "$TMP/问候服务端" > "$TMP/服务端.log" 2>&1 &
QI_PID=$!
# 等它真的听上（最多 10 秒）—— 固定 sleep 在慢机器上会假失败
for _ in $(seq 1 100); do
    nc -z 127.0.0.1 $PORT_QI 2>/dev/null && break
    sleep 0.1
done
if ! nc -z 127.0.0.1 $PORT_QI 2>/dev/null; then
    bad "服务端没起来"
    cat "$TMP/服务端.log"
    exit 1
fi
ok "服务端在听"

echo "▶ qi 客户端"
for one in 问候客户端 流式客户端 压缩验; do
    if GREET_PROTO="$ROOT/协议/greet.proto" GRPC_ADDR=127.0.0.1:$PORT_QI \
        "$TMP/$one" > "$TMP/$one.out" 2>&1 && ! grep -q "✗" "$TMP/$one.out"; then
        ok "$one"
    else
        bad "$one"
        cat "$TMP/$one.out"
    fi
done

if command -v grpcurl > /dev/null 2>&1; then
    echo "▶ grpcurl（走反射，不带 -proto）"
    if grpcurl -plaintext 127.0.0.1:$PORT_QI list 2>/dev/null | grep -q greet.Greeter; then
        ok "反射 list"
    else
        bad "反射 list"
    fi
    if grpcurl -plaintext -d '{"name":"CI"}' 127.0.0.1:$PORT_QI \
        greet.Greeter/SayHello 2>/dev/null | grep -q "你好，CI"; then
        ok "反射调用"
    else
        bad "反射调用"
    fi
else
    echo "▶ 没装 grpcurl，跳过反射验收"
fi

if [ $SKIP_GO -eq 0 ] && command -v go > /dev/null 2>&1; then
    echo "▶ Go 互通（生成桩 + 多路复用 + 流式 + 压缩 + 元数据 + 反射）"
    cd 互通/go对照
    go build -o "$TMP/gosrv" ./server > "$TMP/go构建.log" 2>&1 || {
        bad "Go 对照服务端编译失败"
        cat "$TMP/go构建.log"
    }
    "$TMP/gosrv" -addr 127.0.0.1:$PORT_GO > "$TMP/go服务端.log" 2>&1 &
    GO_PID=$!
    for _ in $(seq 1 100); do
        nc -z 127.0.0.1 $PORT_GO 2>/dev/null && break
        sleep 0.1
    done

    if QI_GRPC_ADDR=127.0.0.1:$PORT_QI go test ./... > "$TMP/go测试.log" 2>&1; then
        ok "Go 客户端 → qi 服务端"
    else
        bad "Go 客户端 → qi 服务端"
        tail -25 "$TMP/go测试.log"
    fi

    cd "$ROOT"
    # 反方向：qi 客户端打真 Go 服务端
    for one in 问候客户端 流式客户端; do
        if GREET_PROTO="$ROOT/协议/greet.proto" GRPC_ADDR=127.0.0.1:$PORT_GO \
            "$TMP/$one" > "$TMP/go_$one.out" 2>&1 &&
            ! grep -q "✗" "$TMP/go_$one.out"; then
            ok "qi $one → Go 服务端"
        else
            bad "qi $one → Go 服务端"
            cat "$TMP/go_$one.out"
        fi
    done
else
    echo "▶ 跳过 Go 互通"
fi

echo
if [ $FAILED -eq 0 ]; then
    echo "验收通过"
else
    echo "验收失败：$FAILED 项"
fi
exit $FAILED
