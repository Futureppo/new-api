package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetBatchTaskRouter(router *gin.Engine) {
	batchTaskRouter := router.Group("/v1/batch")
	batchTaskRouter.Use(middleware.RouteTag("relay"))
	batchTaskRouter.Use(middleware.TokenAuth(), middleware.Distribute())
	{
		batchTaskRouter.POST("/generations", controller.RelayTask)
		batchTaskRouter.GET("/generations/:task_id", controller.RelayTaskFetch)
	}
}
