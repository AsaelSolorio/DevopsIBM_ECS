package main

import (
	"log"
	"net/http"

	"github.com/AsaelSolorio/twelve_factor-app/pkg/weather_consumer"
	"github.com/gin-gonic/gin"
)

func main() {
	log.Println("Starting Weather Consumer...")
	weather_consumer.RunWeatherConsumer()

	r := gin.Default()

	r.GET("/form", func(c *gin.Context) {
		c.HTML(http.StatusOK, "form.html", nil)
	})

	r.Run()
}
