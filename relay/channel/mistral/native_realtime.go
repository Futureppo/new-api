package mistral

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func (a *Adaptor) NativeRealtime(c *gin.Context, info *relaycommon.RelayInfo) (*NativeResult, *types.NewAPIError) {
	upstream, resp, err := channel.DoWssRequestWithResponse(a, c, info)
	if err != nil {
		if resp != nil {
			return a.NativeResponse(c, resp, info)
		}
		return nil, badResponse(err)
	}
	defer upstream.Close()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	client, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return nil, badResponse(err)
	}
	defer client.Close()
	info.IsStream = true
	info.StreamStatus = relaycommon.NewStreamStatus()
	result := &NativeResult{}
	timeout := time.Duration(constant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	stopCancel := context.AfterFunc(c.Request.Context(), func() { upstream.Close(); client.Close() })
	defer stopCancel()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		defer upstream.Close() // Unblock the server reader when the client leaves.
		for {
			kind, data, err := client.ReadMessage()
			if err != nil {
				return
			}
			_ = upstream.SetWriteDeadline(time.Now().Add(timeout))
			if err := upstream.WriteMessage(kind, data); err != nil {
				return
			}
		}
	}()
	defer func() { client.Close(); upstream.Close(); <-clientDone }()
	for {
		_ = upstream.SetReadDeadline(time.Now().Add(timeout))
		kind, data, err := upstream.ReadMessage()
		if err != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
			_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "upstream closed before transcription.done"), time.Now().Add(time.Second))
			return result, badResponse(err)
		}
		info.SetFirstResponseTime()
		terminal, failed := result.observe(data)
		_ = client.SetWriteDeadline(time.Now().Add(timeout))
		if err := client.WriteMessage(kind, data); err != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			return result, badResponse(err)
		}
		if failed {
			err := errors.New("Mistral realtime returned an error event")
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
			info.StreamStatus.RecordError(err.Error())
			return result, badResponse(err)
		}
		if terminal {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
			_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			return result, nil
		}
	}
}
