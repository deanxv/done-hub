package bedrock

import (
	"bytes"
	"done-hub/common/logger"
	"done-hub/common/requester"
	"fmt"
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
		defer func() {
			if r := recover(); r != nil {
				logger.SysError(fmt.Sprintf("Panic in awsStreamReader.processEvents: %v", r))
				logger.SysError(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))
				// processEvents 的 defer 已关闭 channel，此处非阻塞兜底，避免向已关 channel send 二次 panic。
				func() {
					defer func() { _ = recover() }()
					select {
					case s.ErrChan <- fmt.Errorf("stream processing panic"):
					default:
					}
				}()
			}
		}()
		s.processEvents()
	})

	return s.DataChan, s.ErrChan
}

func (s *awsStreamReader) processEvents() {
	// 保证退出时关闭 channel，DrainAndClose 的 drain goroutine 才有终止条件。
	defer close(s.DataChan)
	defer close(s.ErrChan)

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
			return
		}
	}

	// 事件流自然结束：检查 SDK 侧是否有错误（如中途断流）。
	if err := s.stream.Err(); err != nil {
		s.ErrChan <- err
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
