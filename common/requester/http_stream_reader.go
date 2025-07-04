package requester

import (
	"bufio"
	"bytes"
	"done-hub/common/logger"
	"done-hub/types"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/bytedance/gopkg/util/gopool"
)

var StreamClosed = []byte("stream_closed")

type HandlerPrefix[T streamable] func(rawLine *[]byte, dataChan chan T, errChan chan error)

type streamable interface {
	// types.ChatCompletionStreamResponse | types.CompletionResponse
	any
}

type StreamReaderInterface[T streamable] interface {
	Recv() (<-chan T, <-chan error)
	Close()
}

type streamReader[T streamable] struct {
	reader   *bufio.Reader
	response *http.Response
	NoTrim   bool

	handlerPrefix HandlerPrefix[T]

	DataChan chan T
	ErrChan  chan error
}

func (stream *streamReader[T]) Recv() (<-chan T, <-chan error) {
	gopool.Go(func() {
		defer func() {

			if r := recover(); r != nil {
				logger.SysError(fmt.Sprintf("Panic in streamReader.processLines: %v", r))
				logger.SysError(fmt.Sprintf("stacktrace from panic: %s", string(debug.Stack())))

				err := &types.OpenAIError{
					Code:    "system error",
					Message: "stream processing panic",
					Type:    "system_error",
				}

				stream.ErrChan <- err
			}
		}()
		stream.processLines()
	})

	return stream.DataChan, stream.ErrChan
}

//nolint:gocognit
func (stream *streamReader[T]) processLines() {
	for {
		rawLine, readErr := stream.reader.ReadBytes('\n')
		if readErr != nil {
			select {
			case stream.ErrChan <- readErr:
			case <-time.After(1000 * time.Millisecond):
				logger.SysError(fmt.Sprintf("无法发送流错误: %v", readErr))
			}
			return
		}

		if !stream.NoTrim {
			rawLine = bytes.TrimSpace(rawLine)
			if len(rawLine) == 0 {
				continue
			}
		}

		stream.handlerPrefix(&rawLine, stream.DataChan, stream.ErrChan)

		if rawLine == nil {
			continue
		}

		if bytes.Equal(rawLine, StreamClosed) {
			return
		}
	}
}

func (stream *streamReader[T]) Close() {
	stream.response.Body.Close()
}
