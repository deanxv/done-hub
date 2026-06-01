package bedrock

import (
	"bufio"
	"bytes"
	"done-hub/common"
	"done-hub/common/requester"
	"done-hub/types"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream/eventstreamapi"
	"github.com/aws/smithy-go"
)

type streamReader[T any] struct {
	reader   *bufio.Reader
	response *http.Response

	handlerPrefix requester.HandlerPrefix[T]

	DataChan chan T
	ErrChan  chan error

	closeOnce  sync.Once
	recvCalled atomic.Bool
}

func (stream *streamReader[T]) Recv() (<-chan T, <-chan error) {
	stream.recvCalled.Store(true)
	go stream.processLines()

	return stream.DataChan, stream.ErrChan
}

//nolint:gocognit
func (stream *streamReader[T]) processLines() {
	// 保证函数退出时关闭 channel，DrainAndClose 的 drain goroutine 才有终止条件
	defer close(stream.DataChan)
	defer close(stream.ErrChan)

	decode := eventstream.NewDecoder()
	payloadBuf := make([]byte, 0*1024)
	for {
		payloadBuf = payloadBuf[0:0]
		messgae, readErr := decode.Decode(stream.reader, payloadBuf)
		if readErr != nil {
			stream.ErrChan <- readErr
			return
		}

		line, err := stream.deserializeEventMessage(&messgae)
		if err != nil {
			stream.ErrChan <- common.ErrorWrapper(err, "decode_response_failed", http.StatusInternalServerError)
			return
		}

		stream.handlerPrefix(&line, stream.DataChan, stream.ErrChan)

		if line == nil {
			continue
		}

		if bytes.Equal(line, requester.StreamClosed) {
			return
		}
	}
}

// Close 关闭底层响应体并 drain pending channel send，避免 handler 在
// unbuffered channel 上的阻塞 send 导致 producer goroutine 泄漏。
// 实际的 drain + close 顺序逻辑见 requester.DrainAndClose 的注释。
func (stream *streamReader[T]) Close() {
	stream.closeOnce.Do(func() {
		closer := func() {
			if stream.response != nil && stream.response.Body != nil {
				_ = stream.response.Body.Close()
			}
		}

		if !stream.recvCalled.Load() {
			closer()
			return
		}

		requester.DrainAndClose(stream.DataChan, stream.ErrChan, closer, "bedrock streamReader.Close")
	})
}

func (stream *streamReader[T]) deserializeEventMessage(msg *eventstream.Message) ([]byte, error) {
	messageType := msg.Headers.Get(eventstreamapi.MessageTypeHeader)
	if messageType == nil {
		return nil, fmt.Errorf("%s event header not present", eventstreamapi.MessageTypeHeader)
	}

	switch messageType.String() {
	case eventstreamapi.EventMessageType:
		var v BedrockResponseStream
		if err := json.Unmarshal(msg.Payload, &v); err != nil {
			return nil, err
		}
		buffer, err := base64.StdEncoding.DecodeString(v.Bytes)
		if err != nil {
			return nil, err
		}
		return buffer, nil

	case eventstreamapi.ExceptionMessageType:
		exceptionType := msg.Headers.Get(eventstreamapi.ExceptionTypeHeader)
		return nil, errors.New("Exception message :" + exceptionType.String())

	case eventstreamapi.ErrorMessageType:
		errorCode := "UnknownError"
		errorMessage := errorCode
		if header := msg.Headers.Get(eventstreamapi.ErrorCodeHeader); header != nil {
			errorCode = header.String()
		}
		if header := msg.Headers.Get(eventstreamapi.ErrorMessageHeader); header != nil {
			errorMessage = header.String()
		}
		return nil, &smithy.GenericAPIError{
			Code:    errorCode,
			Message: errorMessage,
		}

	default:
		return nil, errors.New("bedrock stream unknown error")
	}
}

func RequestStream[T any](resp *http.Response, handlerPrefix requester.HandlerPrefix[T]) (*streamReader[T], *types.OpenAIErrorWithStatusCode) {
	// 如果返回的头是json格式 说明有错误
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		return nil, requester.HandleErrorResp(resp, requestErrorHandle, true)
	}

	stream := &streamReader[T]{
		reader:        bufio.NewReader(resp.Body),
		response:      resp,
		handlerPrefix: handlerPrefix,

		DataChan: make(chan T),
		ErrChan:  make(chan error),
	}

	return stream, nil
}
