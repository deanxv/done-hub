package bedrock

import (
	"bytes"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"fmt"
	"io"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	brtypes "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	"github.com/bytedance/gopkg/util/gopool"
)

// awsStreamReader 把 SDK 的 InvokeModelWithResponseStream 事件流适配成
// done-hub 的 requester.StreamReaderInterface[string]，供 relay 层统一消费。
// SDK 已经把 AWS 二进制 eventstream 帧解码成一段段 payload chunk（原 Claude JSON 字节），
// 这里只需把每个 chunk 交给 handlerPrefix 处理并推入 DataChan。
type awsStreamReader struct {
	stream        *bedrockruntime.InvokeModelWithResponseStreamEventStream
	handlerPrefix requester.HandlerPrefix[string]

	DataChan chan string
	ErrChan  chan error

	closeOnce  sync.Once
	recvCalled atomic.Bool
}

func newAWSStreamReader(
	stream *bedrockruntime.InvokeModelWithResponseStreamEventStream,
	handlerPrefix requester.HandlerPrefix[string],
) *awsStreamReader {
	return &awsStreamReader{
		stream:        stream,
		handlerPrefix: handlerPrefix,
		DataChan:      make(chan string),
		ErrChan:       make(chan error),
	}
}

func (s *awsStreamReader) Recv() (<-chan string, <-chan error) {
	s.recvCalled.Store(true)
	gopool.Go(func() {
		s.processEvents()
	})

	return s.DataChan, s.ErrChan
}

func (s *awsStreamReader) processEvents() {
	// 终止信号契约：relay/common.go 的消费者用 `case err := <-errChan` 收尾，channel 关闭后
	// 该 case 会读到 nil 并立即命中（select 对已关闭 channel 恒可读），若从未发送过值就直接
	// close，consumer 拿到 nil 会在 err.Error() 上 panic。因此退出前【无论正常结束、SDK 报错，
	// 还是 handlerPrefix panic】都必须先往 ErrChan 送且仅送一个非 nil 终止值，再关闭 channel。
	terminalErr := io.EOF // 默认：流自然结束，补发 io.EOF，与旧手写 eventstream 解码器到流尾返回 io.EOF 的行为一致。
	terminalSent := false

	// defer 执行顺序为 LIFO：先跑「补发终止值」，再 close(ErrChan)、close(DataChan)，
	// 保证终止值一定在 channel 关闭之前送达消费者，panic 路径也不例外。
	defer close(s.DataChan)
	defer close(s.ErrChan)
	defer func() {
		if r := recover(); r != nil {
			// handlerPrefix 处理坏帧等原因 panic：仍要把一个错误终止值交给消费者，
			// 否则空手 close 会让消费者读到 nil 而 panic（与线上那次事故同源）。
			logger.SysError(fmt.Sprintf("Panic in awsStreamReader.processEvents: %v", r))
			logger.SysError(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))
			terminalErr = fmt.Errorf("stream processing panic: %v", r)
		}
		if !terminalSent {
			s.ErrChan <- terminalErr
		}
	}()

	for event := range s.stream.Events() {
		chunk, ok := event.(*brtypes.ResponseStreamMemberChunk)
		if !ok {
			// 非 chunk 事件（未知联合成员等）跳过。
			continue
		}

		line := chunk.Value.Bytes
		s.handlerPrefix(&line, s.DataChan, s.ErrChan)

		if line == nil {
			continue
		}
		if bytes.Equal(line, requester.StreamClosed) {
			// 到此说明 handler（chat 形态的 ClaudeStreamHandler）已自行往 ErrChan 送过 io.EOF，
			// 标记 terminalSent 抑制 defer 的补发，避免向同一消费者重复送终止值。
			terminalSent = true
			return
		}
	}

	if err := s.stream.Err(); err != nil {
		terminalErr = err // 交给 defer 统一发送。
	}
}

// Close 关闭 SDK 事件流并 drain pending channel send，避免 handler 在 unbuffered
// channel 上的阻塞 send 导致 producer goroutine 泄漏。语义见 requester.DrainAndClose。
func (s *awsStreamReader) Close() {
	s.closeOnce.Do(func() {
		closer := func() {
			if s.stream != nil {
				_ = s.stream.Close()
			}
		}

		if !s.recvCalled.Load() {
			closer()
			return
		}

		requester.DrainAndClose(s.DataChan, s.ErrChan, closer, "bedrock awsStreamReader.Close")
	})
}
