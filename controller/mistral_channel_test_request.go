package controller

import (
	"bytes"
	"encoding/binary"
	"io"
	"mime/multipart"

	"github.com/gin-gonic/gin"
)

// A valid two-second silent PCM WAV exercises the transcription endpoint without
// embedding private speech or depending on a third-party sample URL.
func prepareMistralTranscriptionTest(c *gin.Context, model string) error {
	const sampleBytes = 16000 * 2 * 2
	wav := make([]byte, 44+sampleBytes)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 16000)
	binary.LittleEndian.PutUint32(wav[28:], 32000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], sampleBytes)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", model); err != nil {
		return err
	}
	file, err := writer.CreateFormFile("file", "channel-test.wav")
	if err != nil {
		return err
	}
	if _, err := file.Write(wav); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	c.Request.ContentLength = int64(body.Len())
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return nil
}
