package vertex

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

type storageTestTransport func(*http.Request) (*http.Response, error)

func (f storageTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStorageDownloadsNeverForwardCredentialsOutsideGoogle(t *testing.T) {
	service.InitHttpClient()
	client := service.GetHttpClient()
	original := client.Transport
	t.Cleanup(func() { client.Transport = original })
	creds, key := adaptorTestCredentialsJSON(t)
	cacheKey := accessTokenCacheKey(creds, nil)
	accessTokenCache.Store(cacheKey, cachedAccessToken{Token: "storage-token", ExpiresAt: time.Now().Add(time.Hour)})
	t.Cleanup(func() { accessTokenCache.Delete(cacheKey) })
	calls := 0
	client.Transport = storageTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "storage.googleapis.com", r.URL.Host)
		require.Equal(t, "Bearer storage-token", r.Header.Get("Authorization"))
		require.Contains(t, r.URL.EscapedPath(), "a%2Fb.mp4")
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://untrusted.invalid/steal"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	resp, err := FetchStorageObject(context.Background(), "gs://bucket/a/b.mp4", key, "")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, 302, resp.StatusCode)
	require.Equal(t, 1, calls)
	for _, uri := range []string{"https://untrusted.invalid/video", "gs://user:pass@bucket/path", "gs://bucket/path?token=1", "gs://bucket/", "gs://bucket:443/path"} {
		_, err := FetchStorageObject(context.Background(), uri, key, "")
		require.Error(t, err)
	}
	require.Equal(t, 1, calls)
}

func TestMultipartSpeechTranscriptionAndImageMask(t *testing.T) {
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	for _, file := range []struct {
		Name, MIME string
		Data       []byte
	}{{"file", "audio/wav", []byte("RIFFaudio")}, {"image", "image/png", []byte("image")}, {"mask", "image/png", []byte("mask")}} {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", `form-data; name="`+file.Name+`"; filename="sample"`)
		header.Set("Content-Type", file.MIME)
		part, err := writer.CreatePart(header)
		require.NoError(t, err)
		_, err = part.Write(file.Data)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	c, _ := inferenceContext("/v1/audio/transcriptions", upload.String())
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	info := mockInferenceInfo(t, "gemini-3.5-transcribe", "")
	info.RelayMode = relayconstant.RelayModeAudioTranscription
	a := &Adaptor{}
	a.Init(info)
	body, err := a.ConvertAudioRequest(c, info, dto.AudioRequest{ResponseFormat: "json"})
	require.NoError(t, err)
	data, _ := io.ReadAll(body)
	require.Contains(t, string(data), `"audioTranscriptionConfig":{}`)
	require.Contains(t, string(data), `"mimeType":"audio/wav"`)
	result := `{"candidates":[{"content":{"parts":[{"text":"actual transcript"}]}}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"totalTokenCount":15,"promptTokensDetails":[{"modality":"AUDIO","tokenCount":12}]}}`
	responseContext, w := inferenceContext("/v1/audio/transcriptions", "")
	usage, apiErr := a.audioResponse(responseContext, &http.Response{Body: io.NopCloser(strings.NewReader(result))}, info)
	require.Nil(t, apiErr)
	require.Equal(t, 12, usage.PromptTokensDetails.AudioTokens)
	require.Contains(t, w.Body.String(), "actual transcript")
	require.NotContains(t, w.Body.String(), "timestamp")
	info.UpstreamModelName = "imagen-3.0-capability-001"
	info.RelayMode = relayconstant.RelayModeImagesEdits
	info.PriceData.UsePrice = true
	a.Init(info)
	converted, err := a.ConvertImageRequest(c, info, dto.ImageRequest{Prompt: "edit"})
	require.NoError(t, err)
	require.NotNil(t, converted)
}

func TestNativeBatchEmbeddingsAndModalUsage(t *testing.T) {
	info := mockInferenceInfo(t, "gemini-embedding-2", "")
	a := &Adaptor{}
	a.Init(info)
	c, _ := inferenceContext("/v1/models/gemini-embedding-2:batchEmbedContents", `{"requests":[{"model":"ignored-client-model","content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"AQID"}}]},"embedContentConfig":{"documentOcr":false,"autoTruncate":false}}]}`)
	r, err := dto.ParseVertexRequest(c)
	require.NoError(t, err)
	body, err := a.ConvertNativeRequest(c, info, r)
	require.NoError(t, err)
	data, _ := io.ReadAll(body)
	require.Contains(t, string(data), `"documentOcr":false`)
	require.Equal(t, "embedContent", a.action)
	usage, present, err := responseUsage([]byte(`{"usageMetadata":{"promptTokenCount":9,"totalTokenCount":9,"promptTokensDetails":[{"modality":"IMAGE","tokenCount":9}]}}`))
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, 9, usage.PromptTokensDetails.ImageTokens)
	require.Zero(t, usage.PromptTokensDetails.TextTokens)
}
