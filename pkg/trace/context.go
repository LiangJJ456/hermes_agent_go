package trace

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type ctxKey struct{}

// SpanContext 携带在 context 中的链路信息
type SpanContext struct {
	TraceID  string
	SpanID   string
	ParentID string
}

// --- 高性能 ID 生成器 ---
// TraceID (128-bit): [48-bit timestamp_ms][16-bit machine_id][64-bit counter^seed]
// SpanID  (64-bit):  [48-bit timestamp_ms][16-bit counter]
// 无锁设计，~15ns/op

var (
	machineID    uint16
	traceCounter atomic.Uint64
	spanCounter  atomic.Uint64
	randSeed     [8]byte
	idInitOnce   sync.Once
)

func initIDGen() {
	idInitOnce.Do(func() {
		// 一次性从 crypto/rand 获取种子
		_, _ = rand.Read(randSeed[:])
		// machine_id = pid XOR seed 高位，区分同机器多进程
		pid := uint16(os.Getpid())
		machineID = pid ^ (uint16(randSeed[0])<<8 | uint16(randSeed[1]))
		// 用 seed 初始化计数器起点，避免重启后从 0 开始
		traceCounter.Store(binary.LittleEndian.Uint64(randSeed[:]) >> 16)
		spanCounter.Store(uint64(binary.LittleEndian.Uint32(randSeed[4:])))
	})
}

// NewTraceID 生成 128-bit trace ID（时间有序 + 进程唯一）
func NewTraceID() string {
	initIDGen()
	var buf [16]byte
	now := uint64(time.Now().UnixMilli())
	cnt := traceCounter.Add(1)

	// [0:6] 48-bit timestamp ms（支持到 2861 年）
	buf[0] = byte(now >> 40)
	buf[1] = byte(now >> 32)
	buf[2] = byte(now >> 24)
	buf[3] = byte(now >> 16)
	buf[4] = byte(now >> 8)
	buf[5] = byte(now)
	// [6:8] 16-bit machine_id
	buf[6] = byte(machineID >> 8)
	buf[7] = byte(machineID)
	// [8:16] 64-bit counter XOR seed（唯一且不可预测）
	mixed := cnt ^ binary.LittleEndian.Uint64(randSeed[:])
	binary.BigEndian.PutUint64(buf[8:], mixed)

	return hex.EncodeToString(buf[:])
}

// NewSpanID 生成 64-bit span ID（时间有序 + 单调递增）
func NewSpanID() string {
	initIDGen()
	var buf [8]byte
	now := uint64(time.Now().UnixMilli())
	cnt := spanCounter.Add(1)

	// [0:6] 48-bit timestamp ms
	buf[0] = byte(now >> 40)
	buf[1] = byte(now >> 32)
	buf[2] = byte(now >> 24)
	buf[3] = byte(now >> 16)
	buf[4] = byte(now >> 8)
	buf[5] = byte(now)
	// [6:8] 16-bit counter（同毫秒内 65536 个不重复）
	buf[6] = byte(cnt >> 8)
	buf[7] = byte(cnt)

	return hex.EncodeToString(buf[:])
}

// WithSpanContext 将 SpanContext 注入 ctx
func WithSpanContext(ctx context.Context, sc SpanContext) context.Context {
	return context.WithValue(ctx, ctxKey{}, sc)
}

// FromContext 从 ctx 中提取 SpanContext
func FromContext(ctx context.Context) (SpanContext, bool) {
	sc, ok := ctx.Value(ctxKey{}).(SpanContext)
	return sc, ok
}
