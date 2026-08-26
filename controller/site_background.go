/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func GetSiteBackgroundImage(c *gin.Context) {
	settings := system_setting.GetSiteBackgroundSettings()
	if !settings.Enabled {
		c.Status(http.StatusNotFound)
		return
	}

	sourceIndex, err := strconv.Atoi(c.Query("source"))
	if err != nil || sourceIndex < 0 || sourceIndex >= len(settings.Sources) {
		c.Status(http.StatusBadRequest)
		return
	}
	source := settings.Sources[sourceIndex]
	if !source.Enabled {
		c.Status(http.StatusNotFound)
		return
	}

	image, err := service.FetchSiteBackgroundImage(
		source,
		strings.TrimRight(system_setting.ServerAddress, "/"),
	)
	if err != nil {
		logger.LogError(c.Request.Context(), "failed to load configured site background: "+err.Error())
		c.Status(http.StatusBadGateway)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, image.ContentType, image.Data)
}
