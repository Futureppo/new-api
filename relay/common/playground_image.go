package common

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// PlaygroundImageDataURLs converts the form already validated and loaded by
// the playground controller, preserving the order of the uploaded references.
func PlaygroundImageDataURLs(c *gin.Context) ([]string, error) {
	form := c.Request.MultipartForm
	if form == nil {
		return nil, errors.New("reference images are required")
	}
	files := form.File["image[]"]
	if len(files) == 0 {
		files = form.File["image"]
	}
	images := make([]string, 0, len(files))
	for _, header := range files {
		file, err := header.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			return nil, err
		}
		images = append(images, "data:"+http.DetectContentType(data)+";base64,"+base64.StdEncoding.EncodeToString(data))
	}
	return images, nil
}
