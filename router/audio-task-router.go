package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetAudioTaskRouter(router *gin.Engine) {
	audioTaskRouter := router.Group("/v1")
	audioTaskRouter.Use(middleware.RouteTag("relay"))
	audioTaskRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		audioTaskRouter.POST("/audio/generations", controller.RelayTask)
		audioTaskRouter.GET("/audio/generations/:task_id", controller.RelayTaskFetch)
		audioTaskRouter.POST("/music/generations", controller.RelayTask)
		audioTaskRouter.GET("/music/generations/:task_id", controller.RelayTaskFetch)
	}
}
