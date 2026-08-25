package vyceai

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

var errImageComplete = errors.New("VyceAI image complete")

type streamEvent struct {
	Message string          `json:"message"`
	URL     string          `json:"url"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type openAIImageResponse struct {
	Created int64             `json:"created"`
	Data    []openAIImageData `json:"data"`
}

type openAIImageData struct {
	B64JSON string `json:"b64_json"`
}

func handleImageResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	b64JSON, err := consumeImageStream(resp)
	if err != nil {
		return nil, badResponseError(err)
	}

	created := common.GetTimestamp()
	if !info.StartTime.IsZero() {
		created = info.StartTime.Unix()
	}
	response := openAIImageResponse{
		Created: created,
		Data:    []openAIImageData{{B64JSON: b64JSON}},
	}
	data, err := common.Marshal(response)
	if err != nil {
		return nil, badResponseError(fmt.Errorf("marshal OpenAI image response: %w", err))
	}

	info.PriceData.AddOtherRatio("n", 1)
	info.SetFirstResponseTime()
	c.Data(http.StatusOK, gin.MIMEJSON, data)
	return &dto.Usage{}, nil
}

func consumeImageStream(resp *http.Response) (string, error) {
	if resp == nil || resp.Body == nil {
		return "", errors.New("upstream response body is empty")
	}
	defer service.CloseResponseBodyGracefully(resp)

	var completedImage string
	err := consumeSSE(resp.Body, func(eventName string, data []byte) error {
		var event streamEvent
		if err := common.Unmarshal(data, &event); err != nil {
			return fmt.Errorf("invalid upstream event data: %w", err)
		}

		eventType := strings.ToLower(strings.TrimSpace(eventName))
		if isErrorEvent(eventType, event.Error) {
			return fmt.Errorf("upstream error event: %s", streamErrorMessage(event))
		}
		if eventType != "complete" {
			return nil
		}
		if strings.TrimSpace(event.URL) == "" {
			return errors.New("upstream completion event is missing url")
		}

		image, err := base64FromDataURL(event.URL)
		if err != nil {
			return err
		}
		completedImage = image
		return errImageComplete
	})
	if err != nil && !errors.Is(err, errImageComplete) {
		return "", err
	}
	if completedImage == "" {
		return "", errors.New("upstream stream ended before the completion event")
	}
	return completedImage, nil
}

func consumeSSE(reader io.Reader, handle func(eventName string, data []byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxSSEEventBytes)
	eventName := "message"
	dataLines := make([]string, 0, 1)
	dispatch := func() error {
		if len(dataLines) == 0 {
			eventName = "message"
			return nil
		}
		data := []byte(strings.Join(dataLines, "\n"))
		dataLines = dataLines[:0]
		currentEvent := eventName
		eventName = "message"
		return handle(currentEvent, data)
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}
		if strings.HasPrefix(line, "data:") {
			dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read upstream event stream: %w", err)
	}
	return dispatch()
}

func base64FromDataURL(dataURL string) (string, error) {
	trimmed := strings.TrimSpace(dataURL)
	comma := strings.IndexByte(trimmed, ',')
	if comma <= 0 {
		return "", errors.New("upstream completion url is not a Base64 data URL")
	}
	metadata := strings.ToLower(strings.TrimSpace(trimmed[:comma]))
	if !strings.HasPrefix(metadata, "data:image/") || !strings.Contains(metadata, ";base64") {
		return "", errors.New("upstream completion url is not an image Base64 data URL")
	}
	payload := strings.TrimSpace(trimmed[comma+1:])
	if payload == "" {
		return "", errors.New("upstream completion data URL has an empty payload")
	}
	if _, err := io.Copy(io.Discard, base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload))); err != nil {
		return "", fmt.Errorf("upstream completion data URL has invalid Base64: %w", err)
	}
	return payload, nil
}

func isErrorEvent(eventType string, rawError json.RawMessage) bool {
	if eventType == "error" || strings.HasSuffix(eventType, ".error") || strings.HasSuffix(eventType, ".failed") {
		return true
	}
	trimmed := strings.TrimSpace(string(rawError))
	return trimmed != "" && trimmed != "null" && trimmed != "{}"
}

func streamErrorMessage(event streamEvent) string {
	if strings.TrimSpace(event.Message) != "" {
		return strings.TrimSpace(event.Message)
	}
	var message string
	if err := common.Unmarshal(event.Error, &message); err == nil && strings.TrimSpace(message) != "" {
		return strings.TrimSpace(message)
	}
	var object map[string]any
	if err := common.Unmarshal(event.Error, &object); err == nil {
		if value, ok := object["message"]; ok && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return strings.TrimSpace(fmt.Sprint(value))
		}
	}
	return "unknown upstream error"
}

func badResponseError(err error) *types.NewAPIError {
	return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
}
